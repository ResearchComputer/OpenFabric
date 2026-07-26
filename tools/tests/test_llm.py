from openai import OpenAI

client = OpenAI(
    api_key="sk-rc-1nl7Tu6Jdnpi8CBtNsGDI90Ce020RqwO",
    base_url="https://serving-api-ohsqpl3jvq-ew.a.run.app/v1",
)

resp = client.chat.completions.create(
    model="apertus-ai/Apertus-v1.5-70B",
    messages=[{"role": "user", "content": "Hello"}],
)
print(f"Response: {resp.choices[0].message}")