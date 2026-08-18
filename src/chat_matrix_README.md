# Provider matrix test

Runs 3 chat scenarios (count, play → open_media, multi-turn context) against every
LLM provider you opt in via env, so you can check them all with one `go test` instead
of poking the chat by hand.

```sh
export GOBBY_TEST_PROVIDERS="ollama groq gemini"

export GOBBY_TEST_OLLAMA_URL="http://192.168.100.135:11434/v1"
export GOBBY_TEST_OLLAMA_MODEL="qwen3.8:latest"

export GOBBY_TEST_GROQ_URL="https://api.groq.com/openai/v1"
export GOBBY_TEST_GROQ_MODEL="openai/gpt-oss-120b"
export GOBBY_TEST_GROQ_KEY="gsk_..."

export GOBBY_TEST_GEMINI_URL="https://generativelanguage.googleapis.com/v1beta/openai"
export GOBBY_TEST_GEMINI_MODEL="gemini-2.5-flash"
export GOBBY_TEST_GEMINI_KEY="AI..."

go test -run TestProviderMatrix -v -timeout 600s ./src
```

Each provider needs `GOBBY_TEST_<NAME>_URL` (required), `_MODEL`, `_KEY`.
No providers set → the test skips. A failing scenario prints the reply, the tools
used and the `open_id`, so you see exactly what broke and where.
