import asyncio
import aiohttp
import time
import statistics
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

# Endpoints
DIRECT_URL = "http://localhost:8080/v1/echo"
OPENTELA_URL = "http://localhost:8092/v1/service/llm/v1/echo"

# Benchmark Parameters
WARMUP_REQUESTS = 100
BENCHMARK_REQUESTS = 1000
CONCURRENCY = 50

PAYLOAD = {"model": "echo", "message": "Hello, OpenTela Benchmark!", "timestamp": 0}

async def make_request(session, url):
    start = time.perf_counter()
    headers = {"X-Otela-Fallback": "2"}
    try:
        async with session.post(url, json=PAYLOAD, headers=headers) as response:
            await response.read()
            if response.status != 200:
                 return None
    except Exception as e:
        return None
    
    return time.perf_counter() - start

async def worker(session, url, queue, results):
    while True:
        try:
            _ = queue.get_nowait()
        except asyncio.QueueEmpty:
            break
            
        latency = await make_request(session, url)
        if latency is not None:
            results.append(latency)
            
        queue.task_done()

async def run_benchmark(session, url, name, num_requests, concurrency):
    logger.info(f"--- Running {name} Benchmark ---")
    
    # Warmup
    logger.info(f"Warming up with {WARMUP_REQUESTS} requests...")
    for _ in range(WARMUP_REQUESTS):
       await make_request(session, url)
       
    # Benchmark
    logger.info(f"Benchmarking with {num_requests} requests at concurrency {concurrency}...")
    
    queue = asyncio.Queue()
    for i in range(num_requests):
        queue.put_nowait(i)
        
    results = []
    start_time = time.perf_counter()
    
    tasks = []
    for _ in range(concurrency):
        task = asyncio.create_task(worker(session, url, queue, results))
        tasks.append(task)
        
    await asyncio.gather(*tasks)
    end_time = time.perf_counter()
    
    total_time = end_time - start_time
    successful_requests = len(results)
    
    if successful_requests == 0:
        logger.error("All requests failed!")
        return

    rps = successful_requests / total_time
    # Convert latencies to milliseconds
    latencies_ms = [l * 1000 for l in results]
    
    p50 = statistics.median(latencies_ms)
    p90 = statistics.quantiles(latencies_ms, n=100)[89]
    p99 = statistics.quantiles(latencies_ms, n=100)[98]
    avg = statistics.mean(latencies_ms)
    
    print(f"\nResults for {name}:")
    print(f"  Requests successful: {successful_requests}/{num_requests}")
    print(f"  Total time:          {total_time:.2f} s")
    print(f"  Throughput:          {rps:.2f} req/s")
    print(f"  Average Latency:     {avg:.2f} ms")
    print(f"  P50 Latency:         {p50:.2f} ms")
    print(f"  P90 Latency:         {p90:.2f} ms")
    print(f"  P99 Latency:         {p99:.2f} ms\n")

async def main():
    logger.info("Starting Benchmark Script. Please ensure docker-compose-benchmark.yml is running.")
    
    # wait a bit for services to register
    logger.info("Waiting 5 seconds for OpenTela service registry to propagate...")
    await asyncio.sleep(5)
    
    connector = aiohttp.TCPConnector(limit=CONCURRENCY)
    async with aiohttp.ClientSession(connector=connector) as session:
        # 1. Direct Request Baseline
        await run_benchmark(session, DIRECT_URL, "Direct (Baseline)", BENCHMARK_REQUESTS, CONCURRENCY)
        
        # 2. OpenTela Proxied Request
        await run_benchmark(session, OPENTELA_URL, "OpenTela Proxy", BENCHMARK_REQUESTS, CONCURRENCY)


if __name__ == "__main__":
    asyncio.run(main())
