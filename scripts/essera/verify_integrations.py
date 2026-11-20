#!/usr/bin/env python3
"""Checks the integrations the API Docs page lists against a running Essera AI Gateway.

Each check makes one tiny request (a few tokens) through one client and prints PASS or FAIL.
The exit status is 1 when any check fails.

    python3 -m venv /tmp/essera-verify && . /tmp/essera-verify/bin/activate
    pip install -r scripts/essera/requirements-integrations.txt
    export ESSERA_BASE_URL=https://ai-gateway-dev.esseratech.com
    export ESSERA_API_KEY=sk-...                      # a temporary key from the API Keys page
    export ESSERA_MODEL=anthropic.claude-sonnet-4-6   # a chat model the key may call
    export ESSERA_EMBEDDING_MODEL=amazon.titan-embed-text-v2:0   # optional
    python3 scripts/essera/verify_integrations.py

With uv: uv run --no-project --with-requirements scripts/essera/requirements-integrations.txt
scripts/essera/verify_integrations.py

When npm is on the PATH, the OpenAI JavaScript SDK is checked too, from a temporary directory.
"""

import json
import os
import shutil
import subprocess
import sys
import tempfile
import traceback

BASE_URL = os.environ.get("ESSERA_BASE_URL", "").rstrip("/")
API_KEY = os.environ.get("ESSERA_API_KEY", "")
MODEL = os.environ.get("ESSERA_MODEL", "")
EMBEDDING_MODEL = os.environ.get("ESSERA_EMBEDDING_MODEL", "amazon.titan-embed-text-v2:0")
OPENAI_JS_VERSION = "7.52.0"
PROMPT = "Say OK."
MAX_TOKENS = 10


def openai_client():
    from openai import OpenAI

    return OpenAI(base_url=BASE_URL + "/v1", api_key=API_KEY)


def check_openai_models():
    ids = [model.id for model in openai_client().models.list()]
    assert MODEL in ids, f"{MODEL} is not in /v1/models ({len(ids)} models)"
    return f"{len(ids)} models"


def check_openai_chat():
    reply = openai_client().chat.completions.create(
        model=MODEL, messages=[{"role": "user", "content": PROMPT}], max_tokens=MAX_TOKENS
    )
    assert reply.choices, "no choices"
    assert reply.usage and reply.usage.total_tokens > 0, "no usage"
    return repr(reply.choices[0].message.content)


def check_openai_stream():
    stream = openai_client().chat.completions.create(
        model=MODEL,
        messages=[{"role": "user", "content": PROMPT}],
        max_tokens=MAX_TOKENS,
        stream=True,
        stream_options={"include_usage": True},
    )
    text, usage, chunks = "", None, 0
    for chunk in stream:
        chunks += 1
        if chunk.choices and chunk.choices[0].delta.content:
            text += chunk.choices[0].delta.content
        if chunk.usage:
            usage = chunk.usage
    assert chunks > 0, "no chunks"
    assert usage and usage.total_tokens > 0, "no usage chunk"
    return f"{chunks} chunks, {text!r}"


def check_openai_responses():
    response = openai_client().responses.create(model=MODEL, input=PROMPT, max_output_tokens=16)
    assert response.output, "no output"
    return repr(response.output_text)


def check_openai_embeddings():
    result = openai_client().embeddings.create(model=EMBEDDING_MODEL, input=["Hello", "World"])
    assert len(result.data) == 2 and len(result.data[0].embedding) > 0, "no embeddings"
    return f"{len(result.data[0].embedding)} dimensions"


def check_anthropic_messages():
    from anthropic import Anthropic

    client = Anthropic(base_url=BASE_URL, api_key=API_KEY)
    message = client.messages.create(
        model=MODEL, max_tokens=MAX_TOKENS, messages=[{"role": "user", "content": PROMPT}]
    )
    assert message.content, "no content"
    assert message.usage.output_tokens > 0, "no usage"
    return repr(next((block.text for block in message.content if block.type == "text"), ""))


def check_anthropic_stream():
    from anthropic import Anthropic

    client = Anthropic(base_url=BASE_URL, api_key=API_KEY)
    with client.messages.stream(
        model=MODEL, max_tokens=MAX_TOKENS, messages=[{"role": "user", "content": PROMPT}]
    ) as stream:
        text = "".join(stream.text_stream)
        final = stream.get_final_message()
    assert final.usage.output_tokens > 0, "no usage"
    return repr(text)


def check_langchain_chat_openai():
    from langchain_openai import ChatOpenAI

    chat = ChatOpenAI(base_url=BASE_URL + "/v1", api_key=API_KEY, model=MODEL, max_tokens=MAX_TOKENS)
    reply = chat.invoke(PROMPT)
    return repr(reply.content)


def check_langchain_embeddings():
    from langchain_openai import OpenAIEmbeddings

    embeddings = OpenAIEmbeddings(
        base_url=BASE_URL + "/v1", api_key=API_KEY, model=EMBEDDING_MODEL, check_embedding_ctx_length=False
    )
    vector = embeddings.embed_query("Hello")
    assert len(vector) > 0, "empty vector"
    return f"{len(vector)} dimensions"


def check_langchain_chat_anthropic():
    from langchain_anthropic import ChatAnthropic

    chat = ChatAnthropic(base_url=BASE_URL, api_key=API_KEY, model=MODEL, max_tokens=MAX_TOKENS)
    reply = chat.invoke(PROMPT)
    return repr(reply.content)


def check_llamaindex_openai_like():
    from llama_index.core.llms import ChatMessage
    from llama_index.llms.openai_like import OpenAILike

    llm = OpenAILike(
        api_base=BASE_URL + "/v1",
        api_key=API_KEY,
        model=MODEL,
        is_chat_model=True,
        context_window=200000,
        max_tokens=MAX_TOKENS,
    )
    reply = llm.chat([ChatMessage(role="user", content=PROMPT)])
    return repr(reply.message.content)


OPENAI_JS_CHECK = """
import OpenAI from "openai";
const client = new OpenAI({ baseURL: process.env.ESSERA_BASE_URL + "/v1", apiKey: process.env.ESSERA_API_KEY });
const reply = await client.chat.completions.create({
  model: process.env.ESSERA_MODEL,
  messages: [{ role: "user", content: "Say OK." }],
  max_tokens: 10,
});
if (!reply.choices?.length || !(reply.usage?.total_tokens > 0)) throw new Error("no choices or usage");
console.log(JSON.stringify(reply.choices[0].message.content));
"""


def check_openai_js():
    with tempfile.TemporaryDirectory() as directory:
        subprocess.run(
            ["npm", "install", "--silent", "--no-audit", "--no-fund", f"openai@{OPENAI_JS_VERSION}"],
            cwd=directory, check=True, capture_output=True, timeout=300,
        )
        script = os.path.join(directory, "check.mjs")
        with open(script, "w", encoding="utf-8") as file:
            file.write(OPENAI_JS_CHECK)
        done = subprocess.run(["node", script], cwd=directory, capture_output=True, text=True, timeout=120)
        assert done.returncode == 0, done.stderr.strip().splitlines()[-1] if done.stderr.strip() else "node failed"
        return done.stdout.strip()


CHECKS = [
    ("OpenAI Python: models", check_openai_models),
    ("OpenAI Python: chat completion", check_openai_chat),
    ("OpenAI Python: chat completion stream", check_openai_stream),
    ("OpenAI Python: responses", check_openai_responses),
    ("OpenAI Python: embeddings", check_openai_embeddings),
    ("Anthropic Python: messages", check_anthropic_messages),
    ("Anthropic Python: messages stream", check_anthropic_stream),
    ("LangChain: ChatOpenAI", check_langchain_chat_openai),
    ("LangChain: OpenAIEmbeddings", check_langchain_embeddings),
    ("LangChain: ChatAnthropic", check_langchain_chat_anthropic),
    ("LlamaIndex: OpenAILike chat", check_llamaindex_openai_like),
]


def main():
    missing = [name for name, value in (("ESSERA_BASE_URL", BASE_URL), ("ESSERA_API_KEY", API_KEY), ("ESSERA_MODEL", MODEL)) if not value]
    if missing:
        print("Set " + ", ".join(missing) + " (see the top of this file).", file=sys.stderr)
        return 2
    checks = list(CHECKS)
    if shutil.which("npm") and shutil.which("node"):
        checks.append(("OpenAI JavaScript: chat completion", check_openai_js))
    else:
        print("SKIP OpenAI JavaScript: chat completion (no npm/node on PATH)")
    verbose = os.environ.get("ESSERA_VERBOSE") == "1"
    failed = 0
    for name, check in checks:
        try:
            detail = check()
            print(f"PASS {name}: {detail}")
        except Exception as error:  # every failure is reported, then the next check runs
            failed += 1
            print(f"FAIL {name}: {type(error).__name__}: {str(error)[:300]}")
            if verbose:
                traceback.print_exc()
    print(json.dumps({"checks": len(checks), "failed": failed}))
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
