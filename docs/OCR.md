# Receipt OCR — approach decision

Version 2 of the roadmap includes **Receipt OCR**: photograph a supplier or
customer receipt, extract its line items and total, review them, and commit
them as a purchase or a sale.

This document records the decision of *how* to do the OCR, because it is
genuinely different work from everything built so far: an image goes in, and
structured data comes out.

## Decision

**Use a third-party vision LLM through the existing OpenAI-compatible AI
provider — not a self-hosted OCR engine.**

The app already talks to an OpenAI-compatible Chat Completions endpoint
(`AI_BASE_URL`, `AI_API_KEY`, `AI_MODEL`) for the assistant, and the configured
provider (Gemini) is multimodal. Receipt OCR reuses that same client by sending
the image as a base64 `image_url` content part and asking for strict JSON.

### Options considered

| Option | Pros | Cons |
|---|---|---|
| **Third-party vision LLM (chosen)** | Already configured and paid for; reads handwriting and informal receipts; returns structured JSON in one call; no new vendor or key; swap models via env | Per-scan API cost; needs network; accuracy varies, so review is mandatory |
| Dedicated OCR API (Textract, Google Vision, Mindee) | Purpose-built, high raw-text accuracy | New vendor + credentials; returns raw text, so still needs an LLM pass to structure line items; less tolerant of informal layouts |
| Self-hosted (Tesseract / PaddleOCR) | No per-scan cost; offline | Native deps in the container; poor on photos, skew and handwriting; still needs an LLM pass to structure output; more ops burden |

Because the product already pays for an OpenAI-compatible provider and the hard
part on these receipts is *understanding* messy, handwritten line items (not
just reading clean print), a vision LLM gives the best result for the least new
infrastructure.

### Configuration

| Env var | Default | Purpose |
|---|---|---|
| `AI_OCR_MODEL` | value of `AI_MODEL` | Vision-capable model used for scanning. Falls back to `AI_MODEL` when unset. |
| `AI_API_KEY`, `AI_BASE_URL` | as configured for the assistant | Shared with the assistant. |

Switching providers later is an env change: any OpenAI-compatible endpoint that
accepts `image_url` data URLs works.

## Safety: never auto-commit

OCR on handwritten and informal receipts is imperfect, so the flow is
deliberately two-step:

1. `POST /api/v1/receipts/scan` — accepts an image, returns extracted line
   items and the total **without writing anything**. The response also includes
   a best-effort match to existing products.
2. The owner reviews and corrects the data in the Receipts screen.
3. `POST /api/v1/receipts/commit` — the explicit confirmation. Only this call
   writes a purchase or a sale, by delegating to the existing
   `purchases`/`sales` services.

There is no code path from `scan` to a write. If the AI is unreachable or
returns unparseable output, `scan` fails cleanly and nothing is saved.

## Cost and limits

- Uploads are capped (10 MB) and must be an image type; oversized or non-image
  files are rejected before any API call.
- Each scan is one vision request. Failed scans are retried once on transient
  provider errors (rate limits/5xx), matching the assistant client.
