import os
from openevolve import run_evolution, evolve_function
from openevolve.config import Config, LLMModelConfig

config = Config()
config.llm.models = [
    LLMModelConfig(
        name='zai-org/GLM-4.7-Flash',
        api_key=os.environ.get("RC_API_KEY"),
        api_base="https://api.swissai.cscs.ch/v1",
        top_p=0.1,
        temperature=0.7
    )
]

# Evolution with inline code (no files needed!)
result = run_evolution(
    initial_program='''
    def fibonacci(n):
        if n <= 1: return n
        return fibonacci(n-1) + fibonacci(n-2)
    ''',
    evaluator=lambda path: {"score": benchmark_fib(path)},
    iterations=10,
    config=config
)
print(f"Best code: {result.best_code}")