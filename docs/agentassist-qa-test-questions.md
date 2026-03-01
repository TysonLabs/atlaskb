# AtlasKB Test Questions for AgentAssist QA Platform

80 questions with reference answers to validate indexing quality across the AgentAssist QA microservices platform.

---

## Architecture & Service Map

### Q1. What services make up the AgentAssist QA platform and what language is each written in?

The platform is composed of 11 microservices:

| Service | Language | Role |
|---------|----------|------|
| vector-transcribe-ui | PHP | Main application, UI, API, callbacks, queue consumers |
| vector-transcribe-intake | Go | High-volume async audio ingestion with disk durability |
| vector-transcribe-async-api | Go | Async upload API with AMQP queue publishing |
| vector-transcribe-asr-api | Python | Speech-to-text (Faster-Whisper + GPU) |
| vector-transcribe-redaction | Python | PII redaction pipeline (consolidated Go+Python rewrite) |
| vector-transcribe-postback | Go | Decrypts results from S3, delivers to callback.php |
| vector-transcribe-qa | Go | LLM request gateway and queue router |
| vector-transcribe-llm-worker | Python | LLM inference worker (vLLM) |
| vector-encrypt | Go | Envelope encryption service (AWS KMS) |
| vector-transcribe-audio-api | Go | Audio-to-MP3 conversion service |
| vector-transcribe-recording-mover-v2 | Go | SQS-based recording ingestion from S3/SFTP |

---

### Q2. What is the end-to-end flow of an audio recording from upload to completed transcription?

The end-to-end flow has 14 stages:

1. **Ingest** -- Audio arrives via the intake service (high-volume Go ingestion) or direct API calls, all funneled through upload.php.
2. **Convert** -- upload.php converts audio to MP3 via the audio-api service (Go) or local ffmpeg as fallback.
3. **Upload** -- upload.php validates API key, encrypts MP3 to S3, creates media record in MySQL + OpenSearch, sends to async-api.
4. **Queue** -- async-api publishes to the `transcribe.entry` AMQP queue.
5. **Transcription** -- ASR service (Faster-Whisper on GPU) consumes from queue, produces word-level transcript.
6. **Redaction** -- Transcript + audio pass through PII redaction pipeline, publishes to `transcribe.postback`.
7. **Postback** -- Postback service consumes from `transcribe.postback`, decrypts transcript/audio from S3, delivers to callback.php via HTTP POST.
8. **Callback** -- callback.php receives the completed transcript: backs up original audio, replaces with redacted audio, marks media `status=complete`. Discovers tags, fires alerts, determines speaker channels, stores transcript to S3. Fans out to AMQP queues: waveforms, scorecards (30s delay), summary. Submits directly to QA API: pre-asked questions, intents, sentiment.
9. **Scoring** -- Scorecard consumer checks each call against scorecard filter queries in OpenSearch. Matching scorecards submit to QA API -> LLM Worker. Results return via `llm.question_answers_postback` queue -> `scorecard_callback.php`.
10. **Summarization** -- Summary consumer submits to QA API -> LLM Worker -> `summary_callback.php`.
11. **Questions / Intents / Sentiment** -- Direct submissions from callback.php -> QA API -> LLM Worker -> `answer_callback.php`, `intent_callback.php`, `sentiment_callback.php`.
12. **Waveform** -- Audio waveform generated via `audiowaveform` CLI and stored in S3.
13. **Webhooks** -- External systems notified of events via AMQP webhook queue.
14. **Search** -- OpenSearch document updated at upload, callback, scoring, summary, intent, and sentiment stages.

---

### Q3. Which services communicate via AMQP and which use HTTP?

**AMQP (RabbitMQ/LavinMQ) communication:**
- async-api publishes to `transcribe.entry` queue
- intake service publishes to `intake_queue`
- ASR service consumes from `transcribe.input` and publishes to `transcribe.redaction`
- Redaction service consumes from `replacement` queue and publishes to `transcribe.postback`
- Postback service consumes from `transcribe.postback` (default queue name: `postback`)
- QA API publishes to `llm.question_answers` and `llm.question_answers_big`, consumes from `llm.question_answers_postback`
- LLM Worker consumes from `llm.question_answers` / `llm.question_answers_big` and publishes to `llm.question_answers_postback`
- PHP consumers listen on: `scorecards`, `summary`, `waveforms`, `webhooks`, `metadata_ingestion`

**HTTP communication:**
- upload.php calls audio-api for MP3 conversion (`POST /v1/audio/convert`)
- upload.php calls async-api for queue publishing
- All services call vector-encrypt for encryption/decryption (`POST /encrypt`, `/decrypt`, `/encrypt-file-s3`, `/decrypt-file-s3`)
- PHP UI calls QA API for LLM submissions
- Postback service delivers results to callback.php via HTTP POST
- QA API postback consumer delivers LLM results to PHP callback endpoints via HTTP POST

---

### Q4. What databases does the platform use and which services connect to each?

1. **MySQL (Aurora)** -- Primary relational database with read/write split. Connected to by: vector-transcribe-ui (PHP), vector-transcribe-intake (API key validation), vector-transcribe-postback (job metadata), vector-transcribe-qa (API key validation), vector-transcribe-redaction (company info, replacement rules).

2. **OpenSearch** -- Full-text search on `recording` index (`aa-{company_uuid}`), updated at nearly every pipeline stage. Connected to by: vector-transcribe-ui (PHP).

3. **S3** -- Audio files, transcripts, waveforms, metadata. Accessed by almost every service through vector-encrypt.

4. **DocumentDB/MongoDB** -- Job status tracking for the redaction service. Connected to by: vector-transcribe-redaction.

5. **RabbitMQ (LavinMQ)** -- All async processing queues, used by every service except vector-encrypt and audio-api.

---

### Q5. How does the system handle encryption and decryption of audio files and transcripts?

The system uses **envelope encryption** via the centralized vector-encrypt service (Go):

1. Generates a unique **data encryption key (DEK)** per operation.
2. Encrypts the data locally using **AES-256-GCM** with the DEK.
3. Encrypts the DEK itself using an **AWS KMS master key**.
4. Stores both the encrypted data and the encrypted DEK together in a JSON envelope containing: `encrypted_data`, `encrypted_key`, `key_id` (KMS ARN), `encrypt_date`, and tenant metadata.

Per-tenant KMS keys provide multi-tenant isolation. Legacy AES-256-CBC encrypted data (V1 `__E__` prefix, V2 `__EV2__` prefix) is transparently handled via `VectorEncrypt::isEnvelope()` automatic detection. Direct S3 integration via `/encrypt-file-s3` and `/decrypt-file-s3` endpoints.

---

## vector-transcribe-ui (PHP)

### Q6. What are the main entry points in the PHP monolith and what does each handle?

1. **upload.php** (`docroot/api-legacy/upload.php`) -- Single entry point for all audio. Receives multipart POST, validates, converts to MP3, encrypts to S3, creates MySQL/OpenSearch records, sends to async-api.
2. **callback.php** (`docroot/remote/callback.php`) -- Central fan-out point. Receives completed transcription results from postback service, processes them, fans out to waveform/scorecard/summary queues and direct LLM submissions.
3. **LLM Result Callbacks** (`docroot/remote/`) -- Six callback scripts receiving results from QA API postback consumer: `scorecard_callback.php`, `summary_callback.php`, `answer_callback.php`, `sentiment_callback.php`, `intent_callback.php`.
4. **AMQP Queue Consumers** (`owr/scripts/`) -- Long-running PHP processes: `process_automated_scorecards.php`, `process_automated_summary.php`, `process_waveforms.php`, `process_webhooks.php`, `process_metadata_ingestion.php`.
5. **Web UI & Admin Portal** (`docroot/`) -- Smarty-templated admin dashboard and DataTables-based views. Also an agent portal (`agent-docroot/`).
6. **RESTful API** (`docroot/api/`) -- OpenAPI 3.0 documented API with Basic Auth and rate limiting.

---

### Q7. How does upload.php process incoming audio files?

1. Receives multipart HTTP POST with audio file + metadata + API key
2. Validates the API key
3. Calculates SHA1 hash for duplicate detection
4. Converts audio to MP3 (128kbps) via `Audio::convertAndReadMP3()`: primary path calls audio-api service, fallback runs ffmpeg locally
5. Encrypts the MP3 and uploads to S3 (raw + processed copies) via vector-encrypt
6. Creates a media record in MySQL
7. Creates an initial OpenSearch document
8. Sends to async-api (Go) which publishes to the `transcribe.entry` AMQP queue

---

### Q8. What AMQP consumers does the PHP app run and what queues do they listen on?

| Script | Queue | Purpose |
|--------|-------|---------|
| `process_automated_scorecards.php` | `scorecards` | Checks scorecard filters against media in OpenSearch, creates entries, submits to QA API |
| `process_automated_summary.php` | `summary` | Loads transcript, submits to QA API for summarization |
| `process_waveforms.php` | `waveforms` | Downloads audio from S3, generates waveform via `audiowaveform` CLI, uploads JSON to S3 |
| `process_webhooks.php` | `webhooks` | Decrypts message, fires webhook to customer endpoint, retries with delay |
| `process_metadata_ingestion.php` | `metadata_ingestion` | Uploads metadata JSON to S3 |

All consumers follow a resilient pattern: infinite loop with exponential backoff (1s to 60s), connection heartbeat monitoring (30s), unique consumer tags per process, and max 3 retries before dead-lettering.

---

### Q9. How does callback.php handle async results from downstream services?

When the postback service delivers completed transcription results via HTTP POST:

**Step 1 -- `Media::handlePostback()`:**
- If a redacted WAV file was posted: copies current audio in S3 to an `original/` subdirectory as backup, encrypts and uploads redacted audio to replace the original S3 path
- Parses transcript JSON, calculates duration from the last word's `start + length`
- Updates MySQL: `status='complete'`, `dt_complete`, `duration`, `transcript=''`

**Step 2 -- Post-processing:**
- Queues `call_processed` webhook
- Discovers tags via `Tag::discover()` (keyword scanning)
- Determines speaker channels using `determine_agent_channel` keyword group
- Stores transcript to S3 via `Transcript::store()` with channel mapping
- Fires alerts and submits compliance cases based on matched tags

**Step 3 -- Fan-out to async streams:**
- Waveform queue (5s delay, if duration >= 60s)
- Scorecards queue (30s delay, if company has automated scorecards)
- Summary queue (5s delay, if duration >= 90s)
- Direct HTTP to QA API: pre-asked questions, intents (if configured, duration >= 90s), sentiment (if configured, duration >= 120s)

---

### Q10. What are the 6 callback endpoint scripts and what does each one process?

1. **callback.php** -- Transcription Complete. Receives completed transcription from postback service (multipart transcript JSON + optional redacted audio). Central fan-out point: handlePostback, tag discovery, alerts, transcript storage, queue fan-out.

2. **scorecard_callback.php** -- Scorecard Grading Complete. Extracts answers from `<output>...</output>` tags, matches against configured answer options, calculates points, determines Pass/Fail/AutoFail grade, handles recursive scorecards and re-runs.

3. **summary_callback.php** -- Summary Generation Complete. Saves summary via `Media::saveSummary()` and queues a `summary` webhook.

4. **answer_callback.php** -- Pre-Asked Question Answered. Updates question entry via `Media::updateQuestionEntry()` and queues a `question` webhook.

5. **sentiment_callback.php** -- Sentiment Classification Complete. Matches against configured sentiment options using `^SentimentName^` pattern matching (with fallback for compound sentiments like `Negative_to_Positive`).

6. **intent_callback.php** -- Intent Classification Complete. Matches against configured intents using word-boundary regex (`\bIntentName\b`), takes first match, falls back to "Other".

All LLM result callbacks share the same delivery path: LLM Worker -> `llm.question_answers_postback` queue -> QA API postback consumer -> HTTP POST to the callback endpoint.

---

## vector-transcribe-intake (Go)

### Q11. How does the intake service achieve high-volume async audio ingestion?

1. **Asynchronous processing** -- Returns `200 OK` immediately after persisting the job to disk, without waiting for encryption, S3 upload, or AMQP publishing.
2. **Disk durability** -- Jobs are persisted to disk at `/var/lib/vector-intake/processing/{job-id}/` (audio file, metadata, `manifest.json`) before acknowledgment.
3. **Automatic retry** -- A background retrier scans disk every 30s for failed jobs and re-processes them.
4. **Designed for sustained high-volume** -- For customers sending 100k+ calls per day, avoids the synchronous bottleneck of upload.php.

---

### Q12. What is the disk durability mechanism in the intake service?

Before any processing occurs, the job is persisted to disk at `/var/lib/vector-intake/processing/{job-id}/` containing: the audio file, the metadata, and a `manifest.json` file. This ensures that if the process crashes during encryption, S3 upload, or AMQP publishing, the job data survives. A background retrier goroutine scans the directory every 30 seconds for incomplete jobs and retries them automatically.

---

### Q13. How does intake handle backpressure when downstream services are slow?

The service returns immediately to the caller (200/202) after disk persistence, so callers are not blocked by downstream slowness. Jobs accumulate on disk if downstream processing (encryption, S3 upload, AMQP publish) is slow. The background retrier scans every 30s for failed/incomplete jobs. The service effectively uses the local disk as a buffer, decoupling ingestion throughput from downstream processing speed. Specific mechanisms like disk capacity limits or ingestion rate limiting are not documented.

---

### Q14. What happens if the intake service crashes mid-processing -- how is data recovered?

Since jobs are persisted to disk **before** acknowledgment and before downstream processing begins, the audio file, metadata, and manifest survive the crash. When the service restarts, the background retrier scans the processing directory every 30 seconds, discovers incomplete jobs, and automatically re-processes them (encrypting, uploading to S3, and publishing to AMQP). No audio data is lost as long as the disk write completed before the crash.

---

## vector-transcribe-async-api (Go)

### Q15. How does the async API differ from the intake service?

| Aspect | async-api | intake |
|--------|-----------|--------|
| **Queue** | `transcribe.entry` (feeds ASR directly) | `intake_queue` (feeds forwarding consumer) |
| **Disk persistence** | No | Yes (crash recovery) |
| **Retry** | No built-in retry | Background retrier scans every 30s |
| **Use case** | Standard async uploads | Ultra-high-volume with durability guarantees |

The async-api is lighter-weight for standard uploads. The intake service is for ultra-high-volume scenarios (100k+ calls/day) where disk durability and automatic retry are critical.

---

### Q16. What AMQP queues does the async API publish to?

The async API publishes to the `transcribe.entry` AMQP queue. The message contains: S3 bucket and object paths (for encrypted audio), callback URL for results, company ID, media ID, and processing options (diarization settings, priority, skip_redaction flags).

---

### Q17. What is the upload flow through the async API?

1. **HTTP POST** -- Receives audio file + metadata from the client
2. **Validates request** -- Checks API key and required fields
3. **Encrypts audio** -- Calls vector-encrypt service
4. **Uploads to S3** -- Stores the encrypted audio
5. **Publishes AMQP message** to `transcribe.entry` queue with S3 paths, callback URL, company/media IDs, diarization settings, priority, and skip_redaction flags
6. **Returns response** to the caller

---

## vector-transcribe-asr-api (Python)

### Q18. What speech-to-text engine does the ASR service use?

**Faster-Whisper**, a CTranslate2-optimized variant of OpenAI's Whisper model providing ~4x faster inference. Default model is `large-v3-turbo`. Runs on NVIDIA GPUs with CUDA acceleration. Also includes speaker diarization via pyannote, language detection, and a repeat-word guard to prevent hallucinated repeated words.

---

### Q19. How does the ASR service handle GPU resource management?

The service uses NVIDIA GPU with CUDA, per-thread workers for parallel processing across multiple GPU threads, and CTranslate2 optimization for efficient GPU memory utilization. Specific details about GPU memory management or thread pool sizing are not documented beyond the per-thread worker model.

---

### Q20. What audio formats does the ASR service accept?

Audio arriving at the ASR service has already been converted to MP3 (128kbps) by upload.php during ingestion. The upstream audio-api service accepts: WAV, FLAC, MP3, AAC, M4A, OGG for conversion. So while the ASR service primarily receives MP3, the original upload pipeline accepts multiple formats.

---

### Q21. How does Faster-Whisper compare to the standard Whisper model in this deployment?

Faster-Whisper provides **4x faster inference** compared to standard Whisper via CTranslate2 optimization. The default model is `large-v3-turbo`. Detailed quality benchmarks are not documented beyond the speed improvement claim.

---

## vector-transcribe-qa + llm-worker (Go + Python)

### Q22. How does the QA scoring system work end-to-end?

1. **Scorecard assignment** -- After callback.php completes transcript processing, it fetches all enabled automated scorecards for the company. Each scorecard has an `automated_query` (serialized OpenSearch filter).
2. **Filter matching** -- Scorecard UUIDs + filters published to scorecards AMQP queue with 30-second delay. Consumer runs each query as OpenSearch aggregation. If media matches, scorecard is applied.
3. **Deduplication** -- Prevents re-scoring if entry already exists for this scorecard + media.
4. **LLM grading** -- Creates scorecard entry + question entries in MySQL, submits to QA API. Routes to `llm.question_answers` or `llm.question_answers_big` based on token count.
5. **LLM inference** -- LLM Worker calls local vLLM endpoint, encrypts result, publishes to `llm.question_answers_postback`.
6. **Result processing** -- `scorecard_callback.php` extracts answers from `<output>...</output>` tags, matches against configured answer options, records auto_fail/bonus flags, calculates points, determines Pass/Fail grade, handles recursive scorecards and re-runs.

---

### Q23. What is the relationship between vector-transcribe-qa and llm-worker?

**vector-transcribe-qa (Go)** is the **gateway/router**: receives HTTP requests from PHP UI, validates API keys, estimates token count, encrypts payloads, routes to appropriate AMQP queue based on token count. Also runs a postback consumer that decrypts results and delivers to PHP callbacks.

**vector-transcribe-llm-worker (Python)** is the **inference engine**: consumes from AMQP queues, decrypts payloads, calls local vLLM endpoint (`/v1/chat/completions`), handles three request types (`single`, `multi`, `summarize`), encrypts results, publishes to postback queue.

Flow: PHP UI -> QA API (Go) -> AMQP -> LLM Worker (Python) -> AMQP -> QA API postback consumer (Go) -> PHP callback.

---

### Q24. What LLM inference engine does llm-worker use?

**vLLM**, a locally-hosted LLM inference server with an OpenAI-compatible API at `/v1/chat/completions`. All data stays on-premises. Includes a circuit breaker (stops consuming, polls every 15s until healthy) and prompt-too-large self-healing (automatically reroutes oversized standard-queue prompts to the big queue).

---

### Q25. How are QA scorecard prompts structured and evaluated?

**Structure:** Each scorecard contains multiple questions with LLM prompts. Request types include `single` (one question) and `multi` (multiple questions sequentially).

**Evaluation (in scorecard_callback.php):**
1. Extract answer from `<output>...</output>` tags (or first N words as fallback)
2. Match against configured answer options (case-insensitive)
3. Record `auto_fail`, `bonus`, and `rerun` flags from matched answers
4. Calculate `points_scored` and `points_possible`
5. Determine grade: Pass if `points_scored >= points_pass` threshold; Fail if below or any auto_fail triggered
6. Handle recursive scorecards (triggered on Pass/Fail outcomes)
7. Handle re-runs if any answer has `rerun=true`

---

### Q26. How does the QA gateway route requests to the LLM worker?

Token-based queue routing:
- **Requests under 30k tokens** -> `llm.question_answers` queue (standard)
- **Requests 30k+ tokens** -> `llm.question_answers_big` queue (large)

Token count is estimated using a fast O(n) character-based heuristic. This separation prevents large requests from blocking shorter ones. The LLM Worker has prompt-too-large self-healing: if a standard-queue prompt exceeds the model's context window at inference time, it automatically reroutes to the big queue.

---

## vector-encrypt (Go)

### Q27. How does the encryption service implement envelope encryption with AWS KMS?

1. **Key generation** -- Generates a unique data encryption key (DEK) per operation.
2. **Local encryption** -- Encrypts data using AES-256-GCM with the DEK (fast, no size limits).
3. **DEK encryption** -- Encrypts the DEK using AWS KMS master key (only KMS call needed).
4. **Envelope packaging** -- Stores both in a JSON envelope:
```json
{
  "encrypted_data": "<base64-encoded ciphertext>",
  "encrypted_key": "<base64-encoded KMS-encrypted DEK>",
  "key_id": "<KMS key ARN>",
  "encrypt_date": "2025-01-01T00:00:00Z",
  "call_uuid": "<media_uuid>",
  "company_uuid": "<company_uuid>"
}
```

Benefits: performance (only small DEK goes to KMS), per-tenant isolation (per-tenant KMS keys), audit trail (metadata fields), legacy support (auto-detects envelope vs V1/V2 CBC format).

---

### Q28. What is the encrypt/decrypt flow for audio files?

**Encrypt (`/encrypt-file-s3`):**
1. Receive file upload via multipart form + target S3 key
2. KMS `GenerateDataKey` produces a fresh 256-bit DEK (may come from 60-second TTL cache)
3. Fresh 12-byte random nonce generated via `crypto/rand`
4. AES-256-GCM encryption with the plaintext DEK (v2 format: raw bytes, ~33% less overhead than v1)
5. Package envelope JSON (version "v2", nonce, encrypted DEK, KMS ARN, timestamp)
6. Upload to S3 via multipart upload (64MB parts, 16 concurrent streams)
7. Cache write-through to Valkey (ElastiCache) for faster subsequent decryption
8. Zero the plaintext DEK in memory

**Decrypt (`/decrypt-file-s3`):**
1. Download envelope from S3 (or Valkey cache hit)
2. Parse envelope, decode base64 fields
3. Send encrypted DEK to KMS for decryption
4. AES-256-GCM decrypt with recovered DEK + nonce
5. v1: additional base64 decode step; v2: raw plaintext bytes directly
6. Zero the plaintext DEK in memory

---

### Q29. How do other services authenticate with the encryption service?

Services authenticate via the `X-API-Key` HTTP header. Every request must include this header. The key maps to a `TenantConfig` containing the tenant's KMS client, S3 client, allowed KMS key IDs, AWS region, and dedicated AWS SDK clients. Invalid keys return `401 Unauthorized` with no additional information. API keys are never logged (only key length for debugging). Each tenant has completely isolated resources with namespace-scoped caches.

---

### Q30. What happens if AWS KMS is unreachable?

KMS is required for generating new DEKs during encryption and recovering plaintext DEKs during decryption. For encryption, if a valid cached DEK exists (within 60-second TTL and under 10,000 uses), it can be reused without calling KMS, providing a brief buffer during transient outages. For decryption, KMS must be called to unwrap each unique encrypted DEK. All AWS calls respect request cancellation via context. Connection pooling (1024 max idle connections, 512 per host, 90-second idle timeout) minimizes TLS overhead. A KMS error returns HTTP 500 with a generic error message; detailed errors are logged server-side only.

---

## vector-transcribe-postback (Go)

### Q31. How does the postback service deliver transcription results?

1. **Consume** from `transcribe.postback` queue
2. **Validate** JSON structure (malformed = NACK without requeue)
3. **Download and decrypt transcript** from S3 via vector-encrypt (prefers redacted transcript if available)
4. **Download and decrypt redacted audio** (optional, if `redacted_audio` field present)
5. **Query MySQL** (read replica) for job metadata
6. **Update job metadata** (write replica) with `release_tag`, `device`, `transcription_duration`
7. **HTTP POST callback** to `callback_url` as multipart form with: `json` (transcript), `wav_file` (optional redacted audio), `callback_data` (passthrough data)
8. **Mark job complete** in MySQL, set `dt_complete`
9. **Clean up** temporary files
10. **ACK** the AMQP message

---

### Q32. What retry logic does postback use for failed deliveries?

AMQP-level retry via NACK with requeue:
- **HTTP callback failure**: NACK with `requeue=true`, message returns to broker for redelivery
- **Decryption failure**: NACK with `requeue=true`
- **Database error**: NACK with `requeue=true`
- **Malformed JSON or missing callback URL**: NACK *without* requeue (discarded)

No application-level retry logic with exponential backoff. Relies entirely on AMQP broker redelivery. QoS prefetch is 1 per channel.

---

### Q33. What formats can postback deliver results in?

HTTP POST with **multipart form** encoding containing:
- `json` -- The transcript JSON content (decrypted, potentially redacted)
- `wav_file` -- Optional redacted audio file (only if `redacted_audio` was present)
- `callback_data` -- Client-provided passthrough data from `meta_data.callback_data`

---

## vector-transcribe-audio-api (Go)

### Q34. How does the audio API convert recordings to MP3?

Uses **ffmpeg** as an external binary:
1. Receives audio file via multipart form (`POST /v1/audio/convert`)
2. Saves to temporary directory
3. Invokes ffmpeg with: codec `libmp3lame`, configurable bitrate (default 192 kbps), sample rate 44100 Hz
4. Returns converted MP3 with `Content-Type: audio/mpeg`
5. Background janitor cleans up files older than 1 hour every 10 minutes

Supported input: WAV, FLAC, MP3, AAC, M4A, OGG. Stateless service (no database, no S3, no AMQP). Max upload 25 MB, conversion timeout 180s. Falls back to local ffmpeg in PHP app if unavailable.

---

### Q35. What audio processing tools or libraries does it use?

**ffmpeg** (external binary at `/usr/bin/ffmpeg`) with the `libmp3lame` encoder. Configuration loaded from AWS AppConfig with local file caching for resilience. The service is a thin HTTP wrapper around ffmpeg.

---

## vector-transcribe-redaction (Python)

### Q36. What is the full message processing pipeline in the redaction service?

19 ordered steps:

1. **AMQP Consume** -- Message delivered to `Worker._on_message`, wrapped in `wait_for` timeout (default 300s)
2. **Parse** -- Decode UTF-8 JSON into `AMQPData` dataclass
3. **Pre-flight validation** -- Required fields: `id`, `bucket`, `punctuation`, `object_base`
4. **Fetch Job from DocumentDB** -- Get company_id, skip_redaction flag, status. ACK and skip if already `replacement_complete`
5. **Pre-flight company validation** -- Fetch company info and redaction features from MySQL (cached)
6. **Decrypt transcript from S3** -- Parse into `Transcript` dataclass. Short-circuit if no words
7. **Validate word data types** -- Check `word` (str), `start` (number), `length` (number)
8. **Fetch replacement rules from MySQL** -- Priority-ordered, including global rules (cached)
9. **Decrypt audio from S3** -- Write to temp file preserving extension
10. **Apply word/phrase replacements** -- Case-insensitive, multi-word, punctuation-preserving
11. **PII detection in ProcessPoolExecutor** -- scikit-learn pipeline, masks as `[redacted]`
12. **Rebuild transcript** -- Regenerate plaintext and per-channel transcripts
13. **Redact audio via sox** -- Silence-pad redacted segments
14. **Calculate silence metrics** -- Duration and percentage
15. **Repeat validator** -- Check for repeated phrases, flag in DocumentDB
16. **Encrypt and upload redacted transcript to S3**
17. **Encrypt and upload redacted audio to S3** (if audio was redacted)
18. **Publish to postback queue, confirm, update DocumentDB status, ACK**
19. **Post-ACK side-effects** -- Fire-and-forget audit record, Prometheus metrics, temp file cleanup

---

### Q37. How does the PII detection pipeline work and what PII types does it detect?

Uses a **scikit-learn machine learning pipeline** from the `vector_redaction_service` library. Built at import time via `get_full_identification_pipeline()` and stored as `PIPELINE_TEMPLATE` (initialization failure is fatal).

Runs in a **ProcessPoolExecutor** with `max_workers=concurrency` for true CPU parallelism. Process:
1. Pipeline template cloned via `sklearn.base.clone` for thread-safe isolation
2. `fit_transform` runs on input word dictionaries with assembled features list
3. Simple redactor applies redaction decisions, masking as `[redacted]`
4. If cloning fails, pipeline rebuilt from factory as fallback

PII types detected: SSN, credit cards, phone numbers, large numbers, bank account numbers. Base features (SSN, CREDIT_CARD, BANK_ACCOUNT) always included, plus company-specific feature entries from MySQL.

---

### Q38. How does the redaction service handle word/phrase replacements?

Rules are fetched from MySQL (read replica), including both company-specific and global rules (company UUID = all zeros). Results cached with configurable TTL (default 60 seconds). Cleared if `skip_redaction` is set.

Rules are priority-ordered (highest priority first). Application features:
- Case-insensitive matching
- Multi-word phrase matching (spans multiple words)
- Apostrophe normalization during matching
- Punctuation preservation around matched words

Replacements are applied **before** PII detection, so replaced words are not subsequently analyzed for PII.

---

### Q39. What is the concurrency model -- how does it combine asyncio with CPU-bound PII detection?

**Single process, single event loop**: One OS process with one asyncio event loop (optionally uvloop).

**I/O-bound concurrency (asyncio)**: Controlled by AMQP QoS prefetch count (`concurrency`, default 5). Each message handled in its own asyncio task. Async I/O for: encryption calls (httpx), MySQL/DocumentDB queries (aiomysql/motor), audio redaction (async subprocess), AMQP publishing.

**CPU-bound work (ProcessPoolExecutor)**: PII detection runs in separate OS processes with `max_workers=concurrency`, providing true CPU parallelism without GIL contention.

Independent ACK/NACK per message (no ordering dependency). Fire-and-forget tasks for post-ACK side-effects (capped at 500 active tasks).

---

### Q40. How does the ProcessPoolExecutor interact with the asyncio event loop?

PII detection (scikit-learn `fit_transform`) is submitted to the ProcessPoolExecutor via `asyncio.get_event_loop().run_in_executor()`, allowing the event loop to continue processing other messages' I/O operations while CPU-bound PII detection runs in parallel across separate OS processes. Multiple concurrent messages can have PII detection running simultaneously. The executor is shared across all in-flight messages with `max_workers=concurrency`.

---

### Q41. What happens when the process pool crashes (BrokenProcessPool)?

1. **NACK with requeue** -- The current message is returned to the AMQP broker
2. **Pool rebuild** -- The broken pool is replaced with a fresh `ProcessPoolExecutor` via `_rebuild_process_pool()`
3. **No per-message crash counting** -- Requeued to broker for retry via LavinMQ's retry/DLQ mechanism
4. **Metrics tracking** -- Requeue recorded with reason tag `BrokenProcessPool` in `redaction_requeue_reasons_total` Prometheus counter

---

### Q42. How does the encryption circuit breaker work -- what are its thresholds and recovery?

**Trigger**: When an encryption request fails after exhausting 3 retry attempts (exponential backoff: 2s, 4s, 8s). Only 5xx and connection errors trigger retries; 4xx fails fast.

**Backoff**: 30-second backoff window. During this window, all subsequent encryption requests are immediately rejected without HTTP calls.

**Clearing**: (1) Automatic after 30-second window expires; (2) Manual via `reset_circuit_breaker()` called by dependency recovery after successful probe.

**Integration**: `EncryptionError` triggers consumer pause via `_pause_for_recovery()`. Recovery loop probes every 10s (`POST /encrypt` with empty payload). On success, resets circuit breaker and resumes consumer.

---

### Q43. What is the dependency recovery mechanism and how does consumer pausing work?

Generic `_pause_for_recovery(name, probe, on_recovered)` method:

1. **Fail and NACK** -- Message NACKed with requeue, health gauge set to 0
2. **Pause consumer** -- AMQP consumer cancelled, no new messages delivered
3. **Probe loop** -- Background loop probes every 10 seconds:
   - Encryption: `POST /encrypt` with empty payload
   - MySQL: `SELECT 1` on read pool (5s timeout)
   - DocumentDB: `ping` admin command
4. **Recovery** -- On probe success: restore health state, reset circuit breaker (for encryption), re-register consumer

Guards: only one recovery task active at a time; watchdog skips checks during recovery; recovery task cancelled cleanly during shutdown.

---

### Q44. How does the AMQP connection watchdog detect and recover from stale connections?

Background asyncio task at configurable interval (default 60s, `amqp_watchdog_interval`; 0 disables).

**Checks**: (1) AMQP connection is open, (2) AMQP channel is open, (3) consumer tag is registered, (4) idle timeout (default 300s) if no messages received.

**On problem detected**: (1) Drain in-flight messages, (2) Close existing connection/channel, (3) Reconnect (robust connection, channel with publisher confirms, QOS, queue declaration, consumer registration), (4) Increment `redaction_watchdog_reconnects_total`.

**Failure handling**: Exponential backoff on consecutive failures (capped at 300s, failure counter capped at 10). Resets on success. Skips checks during dependency recovery (`_reconnecting` flag).

---

### Q45. What Prometheus metrics does the redaction service expose?

**Gauges:** `redaction_up`, `redaction_in_flight`, `redaction_concurrency`, `redaction_uptime_seconds`, `redaction_amqp_connected`, `redaction_encryption_connected`, `redaction_db_connected`, `redaction_documentdb_connected`, `redaction_pipeline_ready`, `redaction_consumer_active`

**Counters:** `redaction_completed_total`, `redaction_failed_total`, `redaction_requeued_total`, `redaction_requeue_reasons_total` (by reason), `redaction_watchdog_reconnects_total`, `redaction_background_tasks_dropped_total`

**Per-stage timing counters:** `redaction_decrypt_transcript_seconds_total`, `redaction_db_seconds_total`, `redaction_decrypt_audio_seconds_total`, `redaction_replacements_seconds_total`, `redaction_pii_seconds_total`, `redaction_transcript_rebuild_seconds_total`, `redaction_audio_redaction_seconds_total`, `redaction_silence_seconds_total`, `redaction_encrypt_upload_seconds_total`, `redaction_publish_seconds_total`, `redaction_total_seconds_total`

**Histogram:** `redaction_processing_seconds` (buckets: 0.5, 1.0, 2.5, 5.0, 10.0, 25.0, 60.0, 120.0, 300.0)

---

### Q46. What are the health check endpoints and what does each verify?

All on port 8005 (default):

**GET /health/live** -- Always 200. Response: `{"status": "pass"}`. Kubernetes livenessProbe.

**GET /health/ready** -- 200 or 503. Checks: AMQP connected, database connected, encryption connected, DocumentDB connected, pipeline ready, consumer active. Kubernetes readinessProbe.

**GET /health** -- Always 200. Diagnostic JSON: status, in_flight, concurrency, uptime_seconds, consumer_active, jobs_completed, jobs_failed, jobs_requeued, watchdog_reconnects, full checks object.

**GET /metrics** -- Prometheus text exposition format.

---

### Q47. How does AWS AppConfig hot-reload work for the redaction service?

`AppConfigPoller` class:
1. Creates `aioboto3.Session`, calls `start_configuration_session` for initial token
2. Fetches initial snapshot, parsed from INI into flat dict
3. Spawns background poll loop (default 60s interval)
4. Each poll: `get_latest_configuration` with token chaining, respects AWS `NextPollIntervalInSeconds`
5. Empty response body = no changes
6. Non-empty body: parse INI, diff every key against current snapshot
7. Changed keys classified as runtime (instant) or infrastructure (drain-reconfigure-resume)
8. Session recovery with exponential backoff + jitter on token invalidation

---

### Q48. Which configuration keys can be changed at runtime vs requiring a drain-and-reconfigure?

**Runtime (instant):** `cache_ttl` (clears caches), `message_timeout`, `log_level` (re-adds loguru sinks), `redaction_log_level`

**Infrastructure (drain-reconfigure-resume):** `concurrency` (QoS + ProcessPoolExecutor rebuild), `amqp_url/heartbeat/queues/watchdog_interval` (reconnect), `encryption_url/key` (new httpx client), `request_log_*` (new RequestLogger), `db_read_*/db_write_*` (pool swap), `documentdb_*` (Motor client swap), `external_addr` (health server restart), `process_name` (logger context update)

**Not hot-reloadable:** `appconfig_*` keys (controls the poller itself).

---

### Q49. What is the reconfigure sequence when an infrastructure key changes?

1. **Cancel consumer tag** -- No new messages delivered
2. **Drain in-flight** -- Wait up to DRAIN_TIMEOUT, polling `get_in_flight()` every 0.5s
3. **Cancel background tasks** -- Watchdog and throughput stats
4. **Apply changes** -- Per key group: rebuild process pool, reconnect AMQP, create new encryption/DB/DocumentDB clients, restart health server
5. **Re-register consumer** -- Start receiving messages again
6. **Restart watchdog task**
7. **Restart stats task**

The `_reconnecting` flag prevents watchdog interference during reconfiguration.

---

### Q50. How does the configuration priority chain work (env > AppConfig > INI > defaults)?

| Priority | Source | Description |
|----------|--------|-------------|
| 1 (highest) | Environment variables | No prefix, case-insensitive. Cannot be overridden by AppConfig/INI |
| 2 | AWS AppConfig | INI-formatted hosted profile, polled periodically |
| 3 | Local INI file | Pointed to by `CONFIG_INI` env var |
| 4 (lowest) | Code defaults | Hard-coded in Pydantic `BaseSettings` subclass |

First source providing a non-`None` value wins.

---

### Q51. What is the INI section-to-field mapping convention?

Dots in section names become underscores, section name becomes prefix:

| INI Section + Key | Config Key |
|-------------------|------------|
| `[db.read] host` | `db_read_host` |
| `[db.write] host` | `db_write_host` |
| `[amqp] url` | `amqp_url` |
| `[encrypt] url` | `encryption_url` (custom prefix) |
| `[encrypt] key` | `encryption_key` (custom prefix) |
| `[logging] level` | `log_level` (custom prefix) |
| `[worker] concurrency` | `concurrency` (no prefix) |
| `[worker] cache_ttl` | `cache_ttl` (no prefix) |

---

### Q52. How is the redaction service deployed with systemd?

Unit file at `/etc/systemd/system/vector-transcribe-redaction.service`:
- `Type=simple`, `Restart=on-failure`, `RestartSec=10`
- `User=transcribe`, `Group=transcribe`
- `WorkingDirectory=/opt/vector-transcribe-redaction/current` (symlink for atomic deploys)
- `ExecStart=/usr/local/bin/uv run python src/main.py`
- Configuration via `Environment=` directives

CI/CD: Buddy deploys with symlink-based atomic approach: sync to timestamped directory, `uv sync`, S3 backup, symlink swap, systemd restart, Slack notification.

---

### Q53. What external dependencies does the redaction service require (sox, MySQL, DocumentDB, etc.)?

**Infrastructure:** AMQP broker (RabbitMQ/LavinMQ), MySQL (read/write replicas), Vector Encryption Service, AWS S3, DocumentDB/MongoDB (optional), AWS AppConfig (optional), Request Log API (optional)

**System:** Python 3.12+, uv package manager, sox and soxi (validated at startup, fatal if missing), AWS credentials

**Key Python libraries:** vector_redaction_service (scikit-learn PII), aio_pika, aiomysql, motor, httpx, aiohttp, aioboto3, loguru, uvloop (optional)

---

### Q54. How does the redaction service handle audio segment silencing?

Uses **sox** and **soxi** as external subprocesses:
- `soxi` detects audio bitrate (falls back to renaming `.wav` -> `.mp3` if detection fails)
- Redacted segments calculated: start time floored to ms precision, offset 1.5s earlier (pre-padding); duration ceiled to 100ms, plus 0.2s, minimum 1.5s, plus 1.5s pre-padding
- `sox` replaces calculated time segments with silence
- Only runs if any words were actually redacted
- Temp files use `NamedTemporaryFile(prefix=f"redacted-{job_id}-")` for concurrency safety
- Tracked by `redaction_audio_redaction_seconds_total` counter

---

## vector-transcribe-recording-mover-v2 (Go)

### Q55. What is the recording mover's processing pipeline from SQS to intake API?

1. **SQS Receive** -- Long-poll SQS (20s wait, batch up to 5)
2. **Decode SQS Message** -- Native v2 JSON or S3 event notification format
3. **Route** -- `processAudio()` or `processChat()` based on `content_type`
4. **Fetch Media** -- From S3, SFTP, or local filesystem
5. **Fetch Sidecar** (audio only) -- Companion metadata file
6. **Extract Metadata** -- From filename pattern, sidecar, embedded JSON, or combination
7. **Transform** -- split, regex, unix_to_rfc3339
8. **Normalize** -- Field mappings and defaults applied
9. **Upload** -- Streaming `multipart/form-data` POST to intake API via `io.Pipe()`
10. **Post-Process** -- Move to `processed/YYYY/MM/DD/HH/` or `error/YYYY/MM/DD/HH/` in S3
11. **Delete SQS Message** -- Only on success; failed messages return after visibility timeout

Gated by per-company semaphore for concurrency control.

---

### Q56. How does multi-company mode work with dynamic AppConfig reconciliation?

Single mover process manages workers for all configured companies. The Worker Manager (`cmd/mover/manager.go`):
- Polls AppConfig for company configurations (YAML format)
- Reconciles every 10 minutes: stops removed/disabled workers, restarts changed workers, starts new workers
- Each company gets dedicated queue worker with own SQS connection, concurrency semaphore, and configuration
- Graceful shutdown waits up to 30 seconds for in-flight messages
- No restarts needed for config changes

---

### Q57. What storage backends does the mover support (S3, SFTP, local)?

| Backend | Fetch | Move | Notes |
|---------|-------|------|-------|
| **S3** | `GetObject` | `CopyObject` + `DeleteObject` | Per-region client caching |
| **SFTP** | SSH + SFTP download | Not supported | Password or private key auth |
| **Local** | `os.Open` | Not supported | Testing convenience |

Move operations (post-processing) only supported on S3.

---

### Q58. How does the mover extract metadata from filenames vs sidecar files vs embedded JSON?

Configured per-company via `metadata.source`:
- **`filename`**: Pattern like `{AgentID}_{RecordingDate}.wav`, each `{Placeholder}` becomes a field
- **`sidecar`**: Companion file (same basename, different extension) in JSON/CSV/XML, supports dot-path notation for nested fields
- **`both`**: Combines filename and sidecar data
- **`auto`**: Sidecar if SQS message includes metadata location, otherwise filename
- **`embedded`**: For chat JSON, metadata extracted from within the content file via `metadata_key`

After extraction: `field_mappings` remap, `defaults` provide fallbacks, `required` fields validated.

---

### Q59. What transform types are available for metadata fields (split, regex, unix_to_rfc3339)?

1. **`split`** -- Splits string by separator (default: space) into indexed parts. Config: `separator`, `limit` (default 2), targets with `field` and `index`.

2. **`regex`** -- Extracts named capture groups using Go regex `(?P<name>...)`. Each target maps `group` to destination `field`.

3. **`unix_to_rfc3339`** -- Converts Unix timestamp to RFC3339. Auto-detects seconds vs milliseconds. Can overwrite source field in place.

Transforms can be chained sequentially.

---

### Q60. How does the Worker Manager reconcile company configurations every 10 minutes?

On each tick:
1. Fetch all company configs from AppConfig
2. Filter: skip `disabled: true` or missing `queue_url`
3. Stop workers for removed/disabled companies
4. Restart workers whose `queue_url`, `concurrency`, or `content_type` changed
5. Start new workers for newly added companies

AppConfig polls more frequently (15-30s) for fresh data, but the Worker Manager only acts at the 10-minute reconciliation boundary.

---

### Q61. What SQS message formats does the mover support?

**Native v2**: Company embedded in message with explicit media/metadata locations (`type`, `bucket`, `key`).

**S3 event notifications**: Standard AWS format with `Records[].s3.bucket.name` and `Records[].s3.object.key`. Company assigned from worker config (not in message). Sidecar location derived from media path.

File filtering automatic by `content_type`: audio accepts `.wav`/`.mp3`, chat accepts `.json`. Decoder handles both formats transparently.

---

### Q62. How does the streaming multipart upload work via io.Pipe()?

`io.Pipe()` creates a connected reader/writer pair:
- **Writer side**: constructs multipart form fields (`api_key`, `metadata` JSON, `audio` binary stream or base64, extra fields like `diarise`)
- **Reader side**: consumed by HTTP client as request body

Streams data without buffering the entire payload in memory, critical for large audio files. A `DryRunClient` logs the payload instead of sending it for testing.

---

### Q63. What are the normalized metadata fields and how is field resolution ordered?

Fields: `AgentFirstName`, `AgentLastName`, `AgentID`, `CallerNumber`, `Direction`, `Priority`, `PostbackData`, `RecordingDate`

Resolution order:
1. `field_mappings` -- If mapping exists, use mapped source key
2. Exact match -- Field name as-is in raw metadata
3. Case-insensitive match -- Scan all keys

Unrecognized fields passed through as additional key-value pairs.

---

### Q64. How does post-processing work (move to processed/ or error/ prefix)?

Configured per-company under `post_processing:` (S3 only):
- **On success**: Move to `{processed_dir}/YYYY/MM/DD/HH/{filename}` (date from `date_field`, default `RecordingDate`)
- **On failure**: Move to `{error_dir}/YYYY/MM/DD/HH/{filename}`

S3 "move" = `CopyObject` + `DeleteObject`. Chat files use separate directories (`processed_dir_chat`, `error_dir_chat`) with fallback to audio directories.

---

### Q65. What are the three test tiers for the recording mover and what does each cover?

**Tier 1: Local File Test (`TestLocalBittyFile`)** -- Full processor pipeline with local files. No AWS credentials. Tests: config loading, sidecar extraction, all transforms, field mappings, upload payload capture via mock HTTP transport.

**Tier 2: SQS Simulation (`TestSQSSimulation`)** -- Simulates S3 events without real AWS. Mocks S3 with local directory. Tests: S3 event JSON construction, message decode, company assignment, full processor pipeline.

**Tier 3: LocalStack Integration (`TestLocalStackIntegration`)** -- Full end-to-end with LocalStack (Docker). Real S3 uploads, real SQS send/receive, actual `queue.Worker.Run()` with concurrency. Requires Docker Desktop.

Plus unit-level processor tests with `staticLoader` and `captureTransport` covering filename/sidecar metadata, chained transforms, timezone conversion, and base64 encoding.

---

### Q66. How does the mover handle per-region S3 client caching?

The S3 storage backend caches clients by AWS region so that multiple operations targeting the same region reuse the same client rather than creating a new one each time. This improves performance when processing files across multiple S3 buckets in the same region.

---

## Cross-Cutting Concerns

### Q67. How do services discover each other -- is there a service registry or hardcoded URLs?

No dynamic service registry. Discovery via:
1. **Private DNS (Route 53)**: NLBs front services, registered in `aaqaproc.net` zone (e.g., `dev.qa-entry.aaqaproc.net`)
2. **Private DNS (`vectorvent.net`)**: Per-server A records, DNS forwarding via Route 53 Resolver
3. **VPC Endpoints (PrivateLink)**: Cross-account services like `vector-encrypt` accessed via PrivateLink with environment-specific service IDs
4. **Configuration-based URLs**: Service URLs configured via env vars, AppConfig, or INI files pointing to DNS names or PrivateLink endpoints
5. **AMQP broker**: Accessed through CloudAMQP PrivateLink VPC endpoint

---

### Q68. What is the AMQP queue topology across the platform?

| Queue | Producer | Consumer | Purpose |
|-------|----------|----------|---------|
| `intake_queue` | Intake service | Forwarding consumer | High-volume ingestion |
| `transcribe.entry` | Async API, upload.php | ASR Service | Transcription requests |
| `transcribe.redaction` | ASR Service | Redaction Service | PII redaction |
| `replacement` | (configurable name) | Redaction Worker | Redaction input |
| `transcribe.postback` | Redaction Service | Postback Service | Completed results |
| `llm.question_answers` | QA API | LLM Worker | Standard LLM requests (<30k tokens) |
| `llm.question_answers_big` | QA API | LLM Worker | Large LLM requests (30k+) |
| `llm.question_answers_postback` | LLM Worker | QA API postback consumer | Completed LLM results |
| `scorecards` | callback.php (30s delay) | Scorecard consumer | Scorecard evaluation |
| `summary` | callback.php | Summary consumer | Call summarization |
| `waveforms` | callback.php | Waveform consumer | Waveform generation |
| `webhooks` | Various | Webhook consumer | External notifications |
| `metadata_ingestion` | Various | Metadata consumer | Metadata upload |

General flow: `transcribe.entry` -> ASR -> `transcribe.redaction` -> Redaction -> `transcribe.postback` -> Postback -> callback.php -> fan-out.

---

### Q69. How does the platform handle end-to-end encryption of sensitive audio data?

Centralized **envelope encryption** via `vector-encrypt` (Go) with per-tenant AWS KMS keys:
- Unique DEK per operation, AES-256-GCM encryption locally, DEK encrypted by KMS
- JSON envelope stores encrypted data + encrypted DEK + metadata
- Per-tenant KMS keys for multi-tenant isolation
- Every service calls vector-encrypt: upload.php, intake, async-api, ASR, redaction, postback, LLM worker, QA API, webhook consumer, web UI
- Legacy AES-256-CBC format transparently supported
- Direct S3 integration endpoints for encrypt-and-upload / download-and-decrypt

---

### Q70. What monitoring and observability patterns are consistent across Go services?

1. **Health HTTP endpoints**: `/health/live`, `/health/ready`, `/health`, `/metrics`
2. **Prometheus metrics**: Gauges, counters, histograms exposed at `/metrics`
3. **CloudWatch Logs**: All EC2 instances ship to `/agentassist/{env}/application` (30-day retention)
4. **Centralized Syslog**: Forward to `syslog.vectorvent.net:514` (UDP)
5. **Structured logging**: Timestamps, levels, job IDs via systemd-journald
6. **AWS AppConfig integration**: Multiple services (11+) use AppConfig with `AllAtOnce` deployment strategy
7. **Security agents**: CrowdStrike Falcon (EDR), Intruder (vulnerability scanning), Automox (patch management)

---

### Q71. How does AWS AppConfig integrate across multiple services?

All services use three-part identity: Application ID, Environment ID, Configuration Profile ID. `AllAtOnce` instant deployment strategy.

**Recording Mover (Go)**: YAML format, polls frequently (15-30s), Worker Manager reconciles every 10 minutes.

**Redaction Worker (Python)**: INI format, `AppConfigPoller` polls at configurable interval (default 60s), diffs changes, categorizes as runtime/infrastructure, dispatches callbacks. Session recovery with exponential backoff + jitter.

**Services using AppConfig**: redaction, redaction-test-server, redaction-v2, qa, async-api, postback, waveform, audio-api, recording mover.

---

### Q72. What is the deployment strategy -- containers, systemd, or both?

**Primarily systemd** on EC2 instances (Debian 12):
- Go services as compiled binaries
- Python services via `uv run python src/main.py`
- PHP consumers as systemd services (PHP 8.3)
- Template services (`@.service`) for scaling instances

**Some Docker**: CONTROLLER server uses Docker, SOC UI runs as container, QA LLM Controller depends on docker.service, Docker Swarm enabled on controller servers.

**CI/CD**: Buddy with symlink-based atomic deploys (sync, uv sync, S3 backup, symlink swap, systemd restart, Slack notification).

**Infrastructure**: Terraform provisioning, Ansible configuration via SSM.

---

### Q73. How does the platform handle multi-tenant (multi-company) isolation?

1. **Encryption**: Per-tenant AWS KMS keys for envelope encryption
2. **Recording Mover**: Dedicated SQS queue, worker, API key, and metadata rules per company
3. **Database**: Company-specific data isolated via `company_id`/`company_uuid` fields in MySQL
4. **Redaction**: Per-company replacement rules and feature entries (with per-company TTL cache)
5. **AppConfig**: Per-company configurations with separate queue_url, concurrency, api_key, intake_endpoint
6. **QA/LLM**: Per-company API key validation, callback URLs, and scorecard configurations

---

### Q74. What happens to a recording if the redaction service is down?

Recordings are not lost. The system relies on AMQP message requeuing:

1. **Service completely down**: Messages remain in the AMQP queue until service recovers
2. **Running but dependency down**: NACK with requeue, consumer paused, probe loop every 10s, auto-resume on recovery
3. **LavinMQ retry/DLQ**: Built-in retry and dead-letter queue for persistent failures. Zero `requeue=False` paths in redaction worker
4. **Idempotency**: Recovered service skips already-completed jobs (status `replacement_complete` in DocumentDB)

Philosophy: "nothing is deleted, everything is retried"

---

### Q75. What is the error handling philosophy across the platform (retry vs DLQ)?

**Redaction Worker**: "Nothing is deleted, everything is retried." Every error = NACK with `requeue=true`. Zero `requeue=false` paths. Relies on LavinMQ retry/DLQ. Dependency failures trigger consumer pause + probe recovery.

**Postback Service**: More nuanced. Malformed JSON/missing callback URL = NACK without requeue (discarded). Other failures = NACK with requeue.

**Recording Mover**: Failed messages return to SQS after visibility timeout (default 5 min). Only deleted on success.

**LLM Worker**: Circuit breaker (stops consuming, polls every 15s). Prompt-too-large self-healing reroutes to big queue.

**Common patterns**: Fire-and-forget side effects never affect message ACK. Retry with exponential backoff for HTTP calls.

---

## Deep Technical / Edge Cases

### Q76. How does the redaction service's `_pause_for_recovery` prevent multiple concurrent recovery loops?

1. **Single recovery task**: Only one active at a time. If second dependency fails during recovery, call returns immediately (consumer already paused). Second failure caught on next message after first recovery completes.
2. **Watchdog coordination**: Skips health checks during recovery via `_reconnecting` flag.
3. **Clean cancellation**: Recovery task cancelled cleanly during graceful shutdown.

Sequence: NACK message -> set health gauge to 0 -> cancel consumer -> probe every 10s -> on success, run `on_recovered` callback (restore health, reset circuit breaker) -> re-register consumer.

---

### Q77. What is the AMQP QOS prefetch strategy and how does it relate to concurrency?

Prefetch count = `config.concurrency` (default 5):
- Broker delivers at most `prefetch_count` unacknowledged messages simultaneously
- Each message gets its own asyncio task
- ProcessPoolExecutor also uses `max_workers=concurrency`, ensuring pool can handle all concurrent PII detection
- Independent ACK/NACK per message (no ordering dependency)
- Hot-reload: drain, set new QoS, rebuild ProcessPoolExecutor, update health gauge

Postback service (Go) differs: multiple goroutines (default 3) with QoS prefetch of 1 per channel.

---

### Q78. How does the mover handle S3 event notification messages vs native v2 messages differently?

**Native v2**: Contains `company` field, explicit `media` and `metadata` locations with `type`, `bucket`, `key`. Company from message.

**S3 event notifications**: Standard AWS format (`Records[].s3.bucket/object`). Company assigned from worker config (not in message). Sidecar location derived from media path based on company metadata config.

File filtering automatic by `content_type`. Decoder (`message.Decode()`) handles both transparently, producing unified internal representation.

---

### Q79. What happens during a hot-reload of the AMQP URL in the redaction service -- how are in-flight messages handled?

1. **Cancel consumer** -- No new messages
2. **Drain in-flight** -- Wait up to DRAIN_TIMEOUT, polling `get_in_flight()` every 0.5s
3. **Cancel watchdog and stats tasks**
4. **Close AMQP resources** -- Channel and connection with 5s timeouts each
5. **Reconnect** -- New connection with new URL (robust connection, publisher confirms, QOS, queue declaration)
6. **Re-register consumer**
7. **Restart watchdog and stats**

`_reconnecting` flag prevents watchdog interference. **No messages lost** -- in-flight messages finish before old connection closes, unacknowledged messages redelivered by broker.

---

### Q80. How does the redaction service's throughput stats logging work and what does it report?

Background asyncio task running every 60 seconds:

**Tracked**: completed count, failed count, requeued count, cumulative processing duration.

**Output** (INFO level):
```
Throughput: 15 jobs in 60s (15.0/min) | completed=13 failed=0 requeued=2 avg=2.31s in_flight=3
```
When idle:
```
Throughput: 0 jobs in 60s | in_flight=0
```

Includes: total jobs, jobs/min rate, average processing time, breakdown by outcome, current in-flight gauge. Stats reset after each summary. Final stats flush on shutdown captures partial-interval data.
