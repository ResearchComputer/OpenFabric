import openai

client = openai.OpenAI(
    base_url="http://140.238.223.116:8092/v1/service/llm/v1",
    api_key="test-token"
)

response = client.chat.completions.create(
    model="Qwen/Qwen3-1.7B",
    messages=[{"role": "user", "content": "Hello, world!"}]
)
print(response)