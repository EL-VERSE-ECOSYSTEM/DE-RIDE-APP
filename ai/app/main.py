import os
from fastapi import FastAPI, Header, HTTPException
from pydantic import BaseModel, Field

app = FastAPI(title="DE RIDE Amebo AI", version="1.0.0")

class ChatRequest(BaseModel):
    message: str = Field(min_length=1, max_length=4000)
    language: str = "PCM"
    persona: str = "passenger"

@app.get("/healthz")
async def health():
    return {"status": "ok"}

@app.post("/ai/v1/chat")
async def chat(body: ChatRequest, x_internal_service_key: str | None = Header(default=None)):
    secret = os.getenv("INTERNAL_AI_SHARED_SECRET", "")
    if not secret or x_internal_service_key != secret:
        raise HTTPException(401, "invalid internal service key")
    api_key = os.getenv("GROQ_API_KEY", "")
    if not api_key:
        raise HTTPException(503, "AI provider is not configured")
    from groq import AsyncGroq
    client = AsyncGroq(api_key=api_key)
    response = await client.chat.completions.create(
        model=os.getenv("GROQ_CHAT_MODEL", "llama-3.3-70b-versatile"),
        messages=[{"role": "system", "content": f"You are Amebo, a safe Nigerian travel companion. Reply in {body.language}. Never encourage unsafe driving; driver prompts should encourage attention to the road."}, {"role": "user", "content": body.message}],
        max_tokens=int(os.getenv("GROQ_MAX_TOKENS", "500")),
        temperature=float(os.getenv("GROQ_TEMPERATURE", "0.75")),
    )
    return {"reply": response.choices[0].message.content or "", "token_cost": 1}
