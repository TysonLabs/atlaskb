# AtlasKB Test Results — 2026-02-28 22:57

80 questions queried against the indexed AgentAssist QA repos.

---

## Q1. What services make up the AgentAssist QA platform and what language is each written in?

```

Based on the retrieved knowledge, there is no explicit mention of a unified platform named **"AgentAssist QA platform"**. The documents describe several distinct services and repositories, but none are explicitly labeled as part of an "AgentAssist QA platform" or tied together as such.

However, the services mentioned that relate to agents (and could conceivably be part of an agent-focused system) and their **programming languages** are:

- **vector-transcribe-ui** — PHP/JavaScript. A web application for an agent portal managing media transcription, task tracking, and calibration workflows [Fact 6, 13].

- **atlaskb** — No language specified directly, but it exposes an **MCP server** and structured tools for agents, suggesting a backend service. Based on the `atlaskb-typescript-test-repo` example, it may use TypeScript/Node.js, but this is not confirmed in the facts [Fact 2, 9].

- **atlaskb-typescript-test-repo** — TypeScript. A webhook event routing and delivery service built with Express [Fact 9].

- **vector-transcribe-recording-mover-v2** — Go. A configurable, AWS-native service for processing recording transcription requests; uses Gin framework (Go web framework) [Fact 10, 16].

- **owr.scripts::objAgentAlerts**, **owr.scripts::create_agents_from_csv**, **docroot.api::AgentsController**, etc. — PHP. These are services/functions in the vector-transcribe-ui codebase, as indicated by files like `agents.class.php`, `find_overdue_tasks.php`, and `create_agents_from_csv.php` [Facts 3, 4, 5, 19].

Since there is no explicit mention of "AgentAssist QA platform", and no single cohesive architecture or language map is provided for such a platform, the best answer is:

> The retrieved knowledge does not confirm the existence of an "AgentAssist QA platform" or list its constituent services and languages. However, several agent-related services are described across different repositories, each written in different languages (primarily PHP, Go, and TypeScript), but none are explicitly grouped under an "AgentAssist QA platform" label.
```

---

## Q2. What is the end-to-end flow of an audio recording from upload to completed transcription?

```

The end-to-end flow of an audio recording from upload to completed transcription follows a multi-stage, asynchronous pipeline involving several integrated services. Here is the documented sequence of operations:

1. **Upload and Initial Processing**  
   Audio files are initially ingested via an HTTP intake endpoint (e.g., `vector-transcribe-intake` or `vector-transcribe-recording-mover-v2`) which accepts multipart/form-data POST requests containing audio and metadata [vector-transcribe-intake]. The `AudioHandler` in `internal/http/handler/audio.go` manages the full lifecycle of such requests, including upload, processing, and response [http.handler::AudioHandler]. The `uploader::HTTPClient` implementation streams audio via `io.Pipe` to avoid loading the full file into memory and sets appropriate `Content-Type` and `Accept` headers [uploader::HTTPClient].

2. **Encryption, Storage, and Enrichment**  
   Upon intake, audio is encrypted (using an encryption client), stored in S3, and enriched with metadata extracted from filenames or sidecar files [vector-transcribe-intake, vector-transcribe-recording-mover-v2]. Configuration support for multiple transcription providers enables provider-specific behavior [vector-transcribe-recording-mover-v2].

3. **Message Queue Publication**  
   The intake service publishes a message (containing `audioKey` and `metaKey`) to an AMQP queue (e.g., RabbitMQ) for downstream processing [vector-transcribe-intake, amqpconsumer::Consumer]. The `amqpconsumer::Consumer` consumes such messages, downloads and decrypts audio and metadata from S3, and forwards them to the transcription service as multipart/form-data [amqpconsumer::Consumer].

4. **Transcription Service Orchestration**  
   The `asyncapi::API` acts as the central orchestrator of the async transcription pipeline, consolidating dependencies like HTTP client, storage, encryption, DB, config, and logger [asyncapi::API]. A `database::Job` model tracks each transcription job, including flags for diarization, redaction, and priority [database::Job].

5. **Job Submission and Execution**  
   For bulk reprocessing, the `owr.scripts::bulk_transcript_reprocess_from_json` function reads JSON input, extracts media type (e.g., wav/mp3) to determine file content type, and respects the `diarise` and `skip_redaction` fields from media records when submitting to the transcription service [owr.scripts::bulk_transcript_reprocess_from_json]. It also implements retry logic with 100ms delays between requests to avoid overwhelming the service [owr.scripts::bulk_transcript_reprocess_from_json]. Similarly, the `docroot::main` function initiates transcription by calling `Media.start()` with an uploaded file [docroot::main], and `owr.scripts::auto_call_reprocess` re-uploads audio from AWS for media lacking `dt_complete` and triggers reprocessing via `Media->start` [owr.scripts::auto_call_reprocess].

6. **Transcript Generation and Storage**  
   Once transcription completes, the `Transcript::store` function persists the resulting transcript—including speaker mapping, tags, duration, and bank automation results—to the storage layer and search index [Transcript::store]. The `transcript::Result` dataclass holds structured output with fields like `utterances`, `transcript_channels`, `duration`, and `transcribe_duration` [transcript::Result].

7. **Post-Processing and Callbacks**  
   The `vector-transcribe-postback` service consumes jobs from the AMQP queue, decrypts transcripts and optional audio using a circuit-breaker-protected encryption service, and sends HTTP callbacks (with SSRF protection) to user-provided endpoints in multipart or JSON format [vector-transcribe-postback]. Timing analysis across 9 stages—including decrypt_transcript, db, audio, encrypt_upload, and publish—is tracked by `analyze::main` for performance monitoring and concurrency guidance [analyze::main].

This pipeline ensures resilience, scalability, and modularity, supporting configurable hot-reloading, asynchronous message processing, and robust error handling across all stages [vector-transcribe-intake, vector-transcribe-postback].
```

---

## Q3. Which services communicate via AMQP and which use HTTP?

```

Based on the retrieved knowledge, here is a breakdown of which services communicate via **AMQP** (message queue) and which use **HTTP** (synchronous calls or callbacks):

---

### ✅ Services that **communicate via AMQP**

1. **vector-transcribe-intake**  
   - Publishes messages to RabbitMQ (`intake_queue`) using AMQP via the `amqppub::Publisher` interface and `streadway/amqp` [11, 14].  
   - Also contains an AMQP consumer, though its role there is likely administrative or secondary [11].

2. **vector-transcribe-postback**  
   - Consumes from AMQP queues (e.g., RabbitMQ) via `asyncapi::API`, which coordinates AMQP consumption and message routing [3].  
   - Relies on resilient AMQP connectivity and exposes `ConnLost()` for failure notification [7, 12].

3. **vector-transcribe-llm-worker**  
   - Consumes AMQP messages (e.g., from RabbitMQ) using `aiormq` and processes LLM QA jobs [4].

4. **vector-transcribe-qa**  
   - Consumes from AMQP queues, processes jobs via decryption and database updates, and routes based on token count [16, 20].  
   - Uses `asyncapi::API` to manage AMQP connection lifecycle and consumer groups [16].

5. **owr.scripts::process_automated_scorecards**  
   - Entry point for an AMQP consumer that reads automated scorecard tasks from a queue using `AMQPStreamConnection` [6].

6. **owr.scripts::decrypt_media_and_upload**  
   - Consumes from an AMQP source queue and processes decryption/re-upload tasks, republishing to a destination queue [18].

7. **owr.scripts::main** *(in `bank_automation_retry.php`)*  
   - Uses `AMQPStreamConnection` with configurable heartbeat, timeouts, and TLS for retrying SFTP uploads [8].

8. **class.webhook::Webhook**  
   - Uses `AMQPStreamConnection` (via PhpAmqpLib) for asynchronous webhook delivery with encrypted payloads [15].

---

### ✅ Services that **use HTTP**

1. **amqpconsumer::Consumer**  
   - Consumes AMQP messages, then forwards results to a *configurable HTTP endpoint* using `multipart/form-data` [2, 13].

2. **vector-transcribe-postback**  
   - Sends HTTP callbacks (with SSRF protection and support for both multipart and JSON formats) to user-provided endpoints after decryption [3].  
   - Also integrates with encryption service over HTTP (likely for decryption tokens), using a SSRF-safe HTTP client [3].

3. **vector-transcribe-intake**  
   - Uploads encrypted audio/metadata to S3, then publishes to AMQP — but the *original request* is received via HTTP (intake handler) [11].  
   - No explicit HTTP publishing beyond the HTTP intake layer; downstream processing is AMQP-based.

4. **vector-transcribe-llm-worker**  
   - Communicates with vLLM-based model endpoint — likely over HTTP or internal Unix socket (not specified, but `vLLMClient` is referenced) [4].

5. **class.webhook::Webhook**  
   - Also supports *synchronous HTTP execution* of webhooks as an alternative to async AMQP delivery [15].

6. **health::TestHandleReady_Pass**  
   - `/health/ready` endpoint returns HTTP 200 only when AMQP *and encryption* services are connected — implying HTTP is used for readiness checks [10].

---

### 🔄 Hybrid (AMQP + HTTP)

Several services use **both AMQP and HTTP**, depending on the stage of processing:

| Service | AMQP Role | HTTP Role |
|--------|-----------|-----------|
| **vector-transcribe-postback** | Consumes from AMQP queue | Sends results to user endpoint via HTTP callback [3] |
| **vector-transcribe-intake** | Publishes intake messages to RabbitMQ | Receives HTTP intake requests [11] |
| **amqpconsumer::Consumer** | Consumes from AMQP queue | Forwards to transcription endpoint via HTTP [2, 13] |
| **class.webhook::Webhook** | Queues for async delivery via AMQP | Supports synchronous HTTP dispatch [15] |

---

### Summary Table

| Service | AMQP Consumer | AMQP Publisher | HTTP Intake | HTTP Callback/Client |
|--------|---------------|----------------|-------------|----------------------|
| **vector-transcribe-intake** | ✓ (optional) | ✓ | ✓ | — |
| **vector-transcribe-postback** | ✓ | — | — | ✓ |
| **vector-transcribe-llm-worker** | ✓ | — | — | — (→ vLLM endpoint) |
| **vector-transcribe-qa** | ✓ | — | — | — |
| **owr::scorecards** | ✓ | — | — | — |
| **owr::decrypt_media_and_upload** | ✓ | ✓ | — | — |
| **owr::bank_automation_retry** | — | — | — | — (uses AMQP for coordination) |
| **class.webhook::Webhook** | ✓ (async mode) | — | — | ✓ (sync mode) |

Note: “✓” means the service actively participates in that communication mode in production. Some services (e.g., `amqpconsumer::Consumer`) are explicitly designed as AMQP-to-HTTP bridges.
```

---

## Q4. What databases does the platform use and which services connect to each?

```

The platform uses **three databases**: **MySQL**, **DocumentDB (MongoDB-compatible)**, and **PostgreSQL**. Below is a breakdown of which services connect to each, based on the retrieved facts.

---

### **MySQL**
Multiple services and components integrate with MySQL, primarily through either direct `Db` instances or custom abstractions.

- **vector-transcribe-ui**  
  Uses MySQL via the `agent-docroot::Db` abstraction for storing agents, users, media, alerts, tasks, and audit logs [Fact 12].

- **docroot API Keys Page (`api_keys.php`)**  
  Uses a `Db` instance (`objDB`) for database access in API key management [Fact 2, Fact 4].

- **vector-transcribe-redaction**  
  Integrates with MySQL (via async driver, likely `aiomysql` or similar) for job and company metadata [Fact 15].

- **vector-transcribe-qa**  
  Uses MySQL to store API keys, companies, and metadata; supports read/write split via a composite DB router [Fact 3].

- ** owr scripts (e.g., `backfill_missing_uuids.php`, `textract_job_pull.php`) **  
  Use `objDB` (`Db` instances) for database connectivity and company/session/Document processing [Fact 11, Fact 9].

- **Company class (`company_change.php`)**  
  Can be instantiated with a database connection, implying MySQL use for company-specific config loading [Fact 7].

- **Billing class (`billing.class.php`)**  
  Offers MySQL-backed data access methods (e.g., monthly detail, YTD aggregation, distinct periods) [Fact 18].

- **MediaShared class (`media_shared.class.php`)**  
  Uses Medoo ORM for queries involving `media_shared`, `media`, and `agents` tables (strongly implying MySQL) [Fact 19].

- **Scorecard class (`check_scorecard_unarchive.php`)**  
  Depends on database and logger, with querying capabilities (context implies MySQL) [Fact 14].

- **DataTable class (`admin_review_questions.php`)**  
  Depends on DB and logger for database-backed metadata retrieval (again, MySQL in context) [Fact 13].

---

### **DocumentDB (MongoDB-compatible)**
DocumentDB is used in specific services, primarily for document storage with TTL and concurrency support.

- **vector-transcribe-qa**  
  Integrates with DocumentDB for storing API keys, companies, and metadata (in addition to MySQL), using a composite DB router [Fact 3].

- **handlers::IntakeHandler**  
  Depends on database (API key store), encryption client, config, and processing service — while the exact DB isn’t specified here, the architecture in `main.go` shows unified access to both MySQL and DocumentDB, suggesting it may use either or both depending on the repository [Fact 5, Fact 16].

- **database.documentdb::DB**  
  Implements `database.DB` interface with MongoDB/DocumentDB-specific functionality including `Connect`, `Disconnect`, `Reconnect`, and `getCollection` [Fact 6].

- **config::DocumentDBConfig**  
  Defines connection parameters (host, port, user, password, TLS CA file, read preference, database, collection), used for DocumentDB configuration [Fact 10].

- **main.go (application entry point)**  
  Wraps both MySQL and DocumentDB connections using `composite.New` to form a unified database interface — indicating that services built on this infrastructure may target either DB [Fact 16].

---

### **PostgreSQL**
PostgreSQL is used specifically in the internal models layer.

- **models::RepoStore**  
  Uses the `pgx/v5` PostgreSQL driver as its database layer for repository records [Fact 8].  
  This appears to be an internal service layer rather than a web-facing one.

---

### **Summary Table**

| Database     | Services & Components                                                                                     |
|--------------|-----------------------------------------------------------------------------------------------------------|
| **MySQL**    | vector-transcribe-ui, vector-transcribe-qa, vector-transcribe-redaction, docroot/api_keys.php, owr scripts, Billing, MediaShared, Scorecard, DataTable, Company class, main.go (via composite router) |
| **DocumentDB** | vector-transcribe-qa, database.documentdb::DB, main.go (via composite router)                           |
| **PostgreSQL** | models::RepoStore (internal)                                                                              |

Note: The `main.go` entry point and composite DB router suggest services may be designed to work across multiple databases, depending on configuration and use case [Fact 16].
```

---

## Q5. How does the system handle encryption and decryption of audio files and transcripts?

```

The system handles encryption and decryption of audio files and transcripts through a combination of dedicated services, utilities, and standardized cryptographic protocols. Here's how it works across the pipeline:

### Encryption Workflow
- Audio files and transcripts are encrypted using symmetric AES-based schemes:
  - `vectorencryption::Client.DecryptFileS3ToTempPath` and related methods in `internal/vectorencryption/vectorencryption.go` are used to decrypt S3-stored files to temporary paths [Fact 19].
  - `Encrypt::decryptFileS3()` (a static method in the `Encrypt` service) decrypts S3-stored audio during reprocessing workflows [Fact 7].
  - The Python-based `utils::Encryptor` type uses symmetric AES-CBC with HMAC-SHA256 authentication and handles file I/O for encryption/decryption operations [Fact 16].
- Encryption uses AWS KMS for key management: `handlers::EncryptHandlerLarge` generates KMS data keys in parallel and encrypts files with AES-GCM [Fact 12].
- Encrypted content is stored in S3, with prefixes like `__E__` used to indicate encryption status (e.g., in `owr::scripts::main` for bank automation retries) [Fact 10, Fact 14].

### Decryption Workflow
- Decryption typically occurs at the point of use:
  - `agent-docroot::playback` endpoint retrieves encrypted audio from S3, decrypts it using `Encrypt::decrypt`, and streams WAV content with correct HTTP headers and content-length/range support [Fact 2, Fact 4].
  - In the `vector-transcribe-postback` service, jobs consume AMQP messages containing S3 paths to encrypted transcripts and audio, then use a circuit-breaker-protected encryption service to decrypt files concurrently via `errgroup` before sending callbacks [Fact 1, Fact 5].
  - Similarly, `amqpconsumer::Consumer` downloads and decrypts both `audioKey` and `metaKey` from S3 before forwarding messages to the transcription service [Fact 8].

### Integration Points and Constraints
- Encryption/decryption is a defined contract between core components:
  - In `vector-transcribe-redaction`, the `Database` layer must invoke `EncryptionClient` methods to decrypt inputs and encrypt outputs, expecting either valid paths/strings or `EncryptionError` exceptions [Fact 6].
  - Services like `vector-transcribe-redaction`, `vector-transcribe-intake`, and `vector-transcribe-postback` all integrate with S3 as a shared storage layer, where encryption/decryption hooks are applied at upload/download boundaries [Fact 3, Fact 17].

### Observability and Timing
- Timing for decryption stages (e.g., `decrypt_transcript`, `decrypt_audio`) is explicitly tracked and analyzed in `analyze::main` to assess performance bottlenecks and guide concurrency tuning [Fact 11].

In summary, the system implements a layered encryption/decryption approach—combining AES-CBC/GCM encryption, KMS-managed keys, S3 as a secure storage backend, and explicit integration contracts—to ensure confidentiality and reliability of audio and transcript data throughout the pipeline.
```

---

## Q6. What are the main entry points in the vector-transcribe-ui PHP monolith and what does each handle?

```

The retrieved knowledge does not contain information about the **vector-transcribe-ui** PHP monolith or its main entry points.

While several facts reference PHP-related components (e.g., `template_data_loader::TemplateDataLoader` [Fact 7], PHP support added to the `/how` template in `vector-transcribe-intake` [Fact 2]), none describe the structure, entry points, or routing of a *vector-transcribe-ui* monolith.

Additionally, no files or endpoints such as `index.php`, `api.php`, or `admin/` routes are mentioned, nor are any controllers, handlers, or front controllers described.

Therefore: **There is no information available in the provided context about the main entry points of the vector-transcribe-ui PHP monolith or their responsibilities.**
```

---

## Q7. How does upload.php process incoming audio files?

```

The `upload.php` endpoint accepts audio either via a traditional file upload through `$_FILES['audio']` or as base64-encoded data in the POST request body [fact1]. It is a legacy API endpoint designed for media file upload and processing, supporting duplicate detection, transcription job submission, and OpenSearch indexing [fact1].

When the audio is provided as a file upload, the system parses the multipart form and extracts the file from the `'audio'` field [fact2]. For base64-encoded uploads, the audio data is read directly from the POST payload. A test helper function `captureUpload` in `internal/processor/sqs_simulation_test.go` determines the upload type by checking for the presence of files in the `'audio'` field [fact2], indicating that the routing logic distinguishes between these two input formats based on structure.

Internally, uploaded audio is handled uniformly in structures like `simulatedUpload` and `formCapture`, which capture metadata and payload—regardless of whether the audio arrived as a binary file part or as base64 content [fact5, fact8, fact15].

The actual upload behavior beyond ingestion—such as streaming or multipart construction—is implemented in downstream components like `uploader.HTTPClient`, which constructs multipart forms containing the API key, metadata (as JSON), optional extra fields, and the audio—either as a binary file part or a base64-encoded string—based on the input format [fact4, fact9].
```

---

## Q8. What AMQP consumers does the PHP app run and what queues do they listen on?

```

The PHP application runs several AMQP consumers, each listening on specific queues determined via configuration:

1. **`process_metadata_ingestion`**  
   - Listens on an AMQP queue (queue name determined via AMQP config)  
   - Uses `PhpAmqpLib` for consumption with manual acknowledgments [owr.scripts::process_metadata_ingestion]  

2. **`main` (in `bank_automation_retry.php`)**  
   - Listens on the queue configured via `Config::get('amqp', 'webhooks', 'queue')` [owr.scripts::main]  

3. **`AutomatedSummaryConsumer`**  
   - Listens on the queue specified in AMQP config under `amqp.summary.queue` [owr.scripts::AutomatedSummaryConsumer]  

4. **`decrypt_media_and_upload`**  
   - Listens on an AMQP source queue (queue name not explicitly stated, but sourced from AMQP configuration) [owr.scripts::decrypt_media_and_upload]  

Additionally, the `Webhook` class queues payloads asynchronously via AMQP, but does not consume from a queue itself — it publishes to one, using configuration from `Config::get` for connection, queue, and exchange names [class.webhook::Webhook.queueHook, class.webhook::Webhook]  

These consumers are part of a resilient, queue-driven architecture for metadata ingestion, summary processing, media decryption, and webhook retries.
```

---

## Q9. How does callback.php handle async results from downstream services?

```

Based on the provided context, there is no direct entity named `callback.php` listed. However, several callback-related endpoints and functions are documented, and the most relevant ones that likely correspond to `callback.php` behavior are:

- **`docroot.remote::answer_callback`** [Fact 11]: This endpoint handles incoming callback postback data to update media question entries and trigger webhooks. It is described as handling *callback postback data*, updating media question entries, and triggering webhooks — suggesting it processes async results from downstream services by accepting payloads and updating database records accordingly.

- **`docroot.remote::scorecard_callback`** [Fact 9]: Processes scorecard evaluation callbacks, validates inputs, matches responses to expected answers, calculates scores, updates entries, triggers reruns, and executes recursive rules — indicating it consumes async results (likely from a scoring service), persists outcomes, and may initiate follow-up processing.

- **`docroot::filter_groups`** and **`owr.scripts::create_remote_users_from_csv`**, **`create_agents_from_csv`** [Facts 4, 7, 12]: These use `DB::letSync()` after processing to ensure database synchronization, a pattern that may also appear in callback handlers to maintain consistency post-result.

- **`vector-transcribe-qa` service** [Facts 18, 5]: Handles complete queue messages with decryption, callback dispatch, and database updates — and its `asyncapi::API.handleCompleteDelivery` function [Fact 5] marks jobs completed in the database after successful callback.

While the exact `callback.php` is not identified, the **most direct mapping** is to `docroot.remote::answer_callback`, which:

- Accepts incoming callback data (i.e., async results from downstream services),
- Updates media question entries in the database,
- Triggers webhooks as a follow-up action.

It is noted to instantiate `Media`, `Db`, and `Webhook` objects inside the response loop, which may cause performance issues [Fact 11], indicating it processes each callback synchronously in the request handler.

In summary:  
`callback.php` (likely `answer_callback.php`) handles async results by receiving callback payloads, updating relevant database records (e.g., question/media entries), and triggering downstream webhooks — with database consistency ensured by typical `DB::letSync()` usage in similar endpoints, though not explicitly stated for this file.
```

---

## Q10. What are the 6 callback endpoint scripts and what does each one process?

```

Based on the retrieved knowledge, I can identify **three callback endpoint scripts** (not six as asked), each serving distinct purposes in different services:

1. **`docroot/remote/summary_callback.php`** (`summary_callback` endpoint)  
   - Processes incoming POST requests containing transcription summary responses  
   - Validates input, loads media and company context, saves the summary, and queues webhook notifications [^8]

2. **`docroot/api/class/tags.class.php`** (`/tags/all` endpoint)  
   - Handles API requests for retrieving all tags with pagination support  
   - Provides OpenAPI documentation for responses including 200, 401, 429, and 500 [^6]

3. **`owr/scripts/process_webhooks.php`** (`callback` function)  
   - Initially implemented as a placeholder that only logs message body and info-level events  
   - Later replaced with full processing logic for webhook event handling [^10]

Additionally, there are other callback-related endpoints in the system:

4. **`src/api/routes.ts`** (`POST /webhooks/:source` endpoint)  
   - Registers a webhook POST endpoint to receive and process webhook events [^9]

5. **`internal/serverhttp/router.go`** (`POST /intake` endpoint)  
   - One of three endpoints registered by NewRouter, likely used for intake processing (though not explicitly described as a callback) [^7]

6. **`taskflow/api/routes.py`** (six API endpoints defined in one file)  
   - Contains six API endpoints related to tasks, but the specific callback endpoints aren’t individually named or described in the provided facts [^1]

Note: The query asks for six callback endpoint scripts, but only three are explicitly identified as callback endpoints with clear processing responsibilities. The others are general API or intake endpoints.
```

---

## Q11. How does the intake service achieve high-volume async audio ingestion?

```

The intake service (`vector-transcribe-intake`) achieves high-volume async audio ingestion through a combination of **asynchronous messaging**, **modular design**, and **resilient infrastructure**:

1. **RabbitMQ/AMQP-based messaging backbone**: The service uses AMQP for asynchronous communication, where incoming audio files are ingested, encrypted, and stored in S3, then published as messages to a `intake_queue` via `AMQPPublisher` using the `streadway/amqp` library [Fact 15]. This decouples ingestion from downstream processing and enables horizontal scaling.

2. **Asynchronous and resilient message publishing**: AMQP publishing includes retry logic and exponential backoff, ensuring durability even under transient failures [Fact 3]. Dedicated publisher threads are used to avoid blocking and improve throughput [Fact 3].

3. **Modular architecture with separation of concerns**: The service is organized into distinct layers (HTTP handlers, configuration management, encryption, storage, and AMQP components), using dependency injection and clear interfaces [Fact 15]. This modularity supports testability and enables independent scaling of components.

4. **Dynamic configuration**: Settings like API keys and endpoints are loaded from INI/YAML files, environment variables, and optionally AWS AppConfig with hot-reloading support [Fact 14, Fact 15], allowing runtime adaptability without redeployment.

5. **Encryption and storage abstraction**: Audio files are decrypted upon receipt (if encrypted), then re-encrypted before storage in S3—ensuring security while maintaining a pluggable storage layer [Fact 15].

6. **Integration with upstream transcription services**: The `vector-transcribe-recording-mover-v2` service can consume from `vector-transcribe-intake` via configurable HTTP endpoints and SQS/SFTP/S3 sources, enabling end-to-end async workflows [Fact 14].

Together, these mechanisms ensure high throughput, fault tolerance, and scalability for async audio ingestion.
```

---

## Q12. What is the disk durability mechanism in the intake service?

```

The disk durability mechanism in the intake service involves persisting every intake request to a job subfolder and retrying processing until success [Fact 13]. This is part of a "Resumable Job Tracking System" that uses a database-backed `extraction_jobs` table to track each file/PR/issue as an individual job with states such as `pending`, `in_progress`, `completed`, and `failed` [Fact 12]. 

The `IntakeHandler` orchestrates this workflow by processing submitted files through encryption, storage, and metadata generation, and it ensures durability by writing each request to persistent storage before dispatch [Fact 1, Fact 13]. Additionally, the `processing::Manifest` type captures the deterministic state of an intake job, enabling reproducible reprocessing [Fact 11]. 

The `config::ProcessingConfig` controls an always-on disk-based processing and retry system with configurable worker count, supporting this resumable behavior [Fact 3]. These mechanisms together ensure that intake jobs survive service restarts and can be retried autonomously.
```

---

## Q13. How does intake handle backpressure when downstream services are slow?

```

The intake service handles backpressure through a combination of **AMQP-level consumer controls** and **retry-based resilience mechanisms**, rather than relying solely on in-process buffering.

Specifically:

- **AMQP prefetch control**: The `src::Worker` type (used in related services like `vector-transcribe-redaction`) implements backpressure via AMQP prefetch count, limiting how many unacknowledged messages a consumer can hold at once [Fact 3]. This ensures the consumer doesn’t get overwhelmed when downstream processing is slow.

- **Retry loops with delays**: In `vector-transcribe-intake`, errors during publishing or processing trigger a **5-second delay retry loop**, allowing time for downstream services to recover before re-attempting [Fact 9].

- **Exponential backoff with jitter**: While not directly attributed to intake, the broader transcribe stack (e.g., `vector-transcribe-qa`) uses exponential backoff with ±25% jitter to prevent synchronized reconnect storms during AMQP failures [Fact 10]. This pattern is likely reused across services, including intake, for resilience against intermittent downstream slowness.

- **Graceful shutdown and draining**: Services use draining logic during reconfiguration (e.g., in `tests::TestReconfigure`) to wait for in-flight tasks to complete, preventing message loss during rebalancing [Fact 12]. Similarly, MySQL connections enforce a 30-second timeout (`drainTimeout`) during shutdown [Fact 7].

There is **no evidence of large in-process buffers** or message dropping in the intake service itself. Instead, it relies on the **inherent backpressure mechanisms of AMQP** (via prefetch and acknowledgments) and **external retry/recovery patterns** to manage slow downstream consumption.

Note: Other services like `atlaskb-test-repo` use channel-based dispatch with *dropping on full buffers*, but this is not documented for the intake service and likely represents a different architectural choice [Fact 8].
```

---

## Q14. What happens if the intake service crashes mid-processing - how is data recovered?

```

If the intake service crashes mid-processing, data recovery is handled through several coordinated mechanisms:

1. **Event Persistence Before Processing**: The event bus ([`bus::Bus`](pkg/bus/bus.go)) persists events *before* dispatching them to channels, which prevents data loss in case of a crash [Fact 6].

2. **Job State Manifests**: The [`processing::Manifest`](internal/processing/manifest.go) captures the deterministic state of an intake job—including metadata, S3 keys, and retry information—enabling reproducible reprocessing after a crash [Fact 9].

3. **Panic Recovery in Request Handling**: Both [`serverhttp::NewRouter`](internal/serverhttp/router.go) and [`serverhttp::NewEngine`](internal/serverhttp/router.go) use `gin.Recovery()` middleware to catch panics during request handling, preventing crashes from terminating the process [Facts 1, 2].

4. **Safe Invocation of Subscribers**: The [`config::safeInvoke`](internal/config/appconfig_manager.go) function uses `recover()` to catch and log panics from configuration subscribers without crashing the update flow [Fact 7].

5. **Error Handling with Retry Logic**: The [`vector-transcribe-intake`](vector-transcribe-intake) service implements a retry loop with 5-second delays for errors during publishing or processing, and uses `safeInvoke` to recover from panics in subscriber callbacks [Fact 3].

6. **Graceful Shutdown with Timeout**: The [`database.mysql::DB`](internal/database/mysql/api_key.go) implementation enforces a 30-second timeout (`drainTimeout`) to allow in-flight operations to complete during shutdown or crash recovery [Fact 16].

7. **Result Channels for External Recovery**: The [`worker::WorkerPool.Results`](pkg/worker/pool.go) function exposes job outcomes (success or failure) via a read-only channel, allowing external code to inspect and recover from partial failures [Fact 17].

In summary, the system ensures crash resilience through event persistence, job state manifests, panic recovery instrumentation, and retry logic—collectively supporting durable, reproducible intake workflows even after a mid-processing crash.
```

---

## Q15. How does the async API differ from the intake service?

```

Based on the retrieved facts, the **async API** and the **intake service** serve distinct roles in the architecture:

---

### **Async API**
- The async API is implemented in the `vector-transcribe-async-api` service [vector-transcribe-async-api].
- It follows a **Dependency Injection + Composite Service** pattern, aggregating multiple services (HTTP client, storage, config, database, encryption) via constructor injection to orchestrate high-level async workflows [vector-transcribe-async-api].
- Its public API exposes methods such as `Download`, which delegates to the storage layer for file retrieval [asyncapi::API.Download].
- It interacts with other services through well-defined contracts:
  - Its HTTP client is configured with a 60s timeout and `InsecureSkipVerify: true` [vector-transcribe-async-api].
  - Its callback stage requires an encryption client capable of decrypting S3 files and an HTTP callback client that produces multipart or JSON requests to validated URLs [vector-transcribe-async-api, vector-transcribe-postback].
- SSRF protection is enforced—callback URLs must be validated before being passed to the HTTP client [vector-transcribe-async-api, vector-transcribe-postback].

In essence, the async API is responsible for **orchestrating end-to-end asynchronous transcription workflows**, including encryption, storage, callbacks, and error handling.

---

### **Intake Service**
- The intake service is implemented in the `vector-transcribe-recording-mover-v2` service [vector-transcribe-recording-mover-v2].
- It is a **configurable AWS-native service** that accepts SQS messages referencing media files (stored in S3, SFTP, or local filesystems), enriches them with metadata (e.g., from filenames or sidecar files), and uploads them to an **intake endpoint** (configurable via `api_key` and `endpoint` in config) [vector-transcribe-recording-mover-v2].
- It uses INI/YAML files, environment variables, and optionally AWS AppConfig for dynamic configuration, enabling hot updates without redeployment.
- It includes **robust error handling, metadata normalization, and graceful shutdown logic** [vector-transcribe-recording-mover-v2].
- Integration points include HTTP intake endpoints and file storage systems (e.g., S3, SFTP) [vector-transcribe-recording-mover-v2].

In contrast, the intake service is focused on **ingesting and preparing media files** for downstream processing, acting as a **producers’ entry point** rather than a workflow orchestrator.

---

### Key Differences Summary

| Aspect | Async API | Intake Service |
|-------|-----------|----------------|
| **Role** | Orchestrates asynchronous transcription workflows (encryption → storage → callback) | Ingests media files and uploads them to an intake endpoint |
| **Entry Mechanism** | HTTP endpoint (for transcription jobs), AMQP (for postbacks) | SQS messages referencing media files |
| **Architecture Pattern** | Composite Service with Dependency Injection | AWS-native, message-driven ingestion service |
| **Dependencies** | HTTP client, storage, config, database, encryption clients | File storage (S3/SFTP/local), HTTP intake endpoint, config |
| **Workflow Stage** | Middle-to-late (coordination, callbacks) | Early (file ingestion and prep) |

Thus, the **intake service feeds into** the async API: it prepares and uploads media files, which are then processed by the async API’s orchestration logic.
```

---

## Q16. What AMQP queues does the async API publish to?

```

Based on the retrieved facts, the async API (specifically `asyncapi::API`) publishes to the **`transcribe.entry`** queue.

This is confirmed by **Fact 12**, which states that `asyncapi::data` (a data structure used by the async API) is “marshaled to JSON and published as AMQP message body to **transcribe.entry queue**”.

Additionally, **Fact 1** indicates that `asyncapi::API` must obey configuration from `config::Manager`, including AMQP queue names—though the exact queue names are config-driven, the explicit example given in the facts is `transcribe.entry`.

No other specific queue names are directly attributed to `asyncapi::API` in the provided facts.
```

---

## Q17. What is the upload flow through the async API?

```

The upload flow through the async API follows a **request-response with deferred side effects** architectural pattern [Fact 8]. Here's how it works:

1. **Initial Synchronous Request**: A caller invokes `QueueWavFile`, which returns a `QueuedRequest` synchronously—providing an immediate response [Fact 8].

2. **Asynchronous Side Effects Triggered Post-Response**: After returning the synchronous response, the system triggers several asynchronous steps:
   - **Encryption** of the data
   - **S3 upload** of the encrypted file
   - **LavinMQ publish** (likely to a message queue)
   - **Status upload** to indicate completion or failure [Fact 8]

3. **Failure Mitigation Strategy**: If any of the asynchronous steps fail, the system mitigates failures by persisting errors (e.g., writing to an error directory) rather than performing immediate rollback [Fact 8].

4. **Callback Mechanism**: When transcription completes, the `vector-transcribe-postback` service handles delivering results via HTTP callbacks to user-provided endpoints. This involves:
   - Consuming jobs from an AMQP queue [Fact 3]
   - Decrypting files using an encryption service protected by circuit breakers [Fact 3]
   - Sending callbacks in either multipart or JSON format, with SSRF protection and URL validation [Fact 3]

5. **Public API Exposure**: The `asyncapi::API.Download` method provides an HTTP handler for download requests, delegating to the storage layer’s `Download` method [Fact 2].

6. **Logging and Observability**: The system exposes Prometheus metrics and health endpoints for observability, with support for config hot-reloading and selective service restarts [Fact 3].

In summary, the async API decouples the request acknowledgment from the underlying processing pipeline, prioritizing responsiveness and resilience through background task handling, queue-based delivery, and structured failure recovery.
```

---

## Q18. What speech-to-text engine does the ASR service use?

```

The ASR service uses **OpenAI Whisper** as its core speech-to-text engine [18]. This is further supported by Pyannote for diarization, enabling speaker labeling alongside transcription [1, 18]. The integration with Whisper is implemented both via gRPC and local inference, depending on deployment configuration [18].
```

---

## Q19. How does the ASR service handle GPU resource management?

```

The ASR service implements a multi-layered, VRAM-aware GPU resource management system designed for production stability and efficient concurrency. Here's how it works:

### 1. **GPU Worker Pool with CUDA Streams**  
The `GPUWorkerPool` [gpu::GPUWorkerPool] manages concurrent GPU work using a deque of `torch.cuda.Stream` objects, enforcing a VRAM safety margin before launching tasks [Fact 3, 11]. It logs warnings with free VRAM (MB) and queue depth when tasks are blocked [Fact 11], and maintains a `_blocked_tasks` queue and `_guard_active` flag to suspend/resume execution when VRAM is low [Fact 14]. After each task finishes, it calls `_maybe_run_blocked_task()` to resume queued tasks if sufficient VRAM becomes available [Fact 8].

### 2. **Dynamic Batch Sizing**  
The service uses `pick_batch_size` [transcribe.fw::pick_batch_size], which determines optimal batch size based on available VRAM via `torch.cuda.mem_get_info`, respecting a global `ENABLE_DYNAMIC_BATCH` flag [Fact 5, 17]. When dynamic batching is disabled, it returns the requested value without inspection [Fact 17].

### 3. **VRAM Allocation Modeling**  
`TranscriberConfig` models VRAM usage with a per-item allocation of **90 MB** and a **1024 MB safety reserve** [Fact 6]. The runtime config [config::config] uses `PER_ITEM_VRAM_MB` (default **75 MB**) to support dynamic batching when `ENABLE_DYNAMIC_BATCH=1` [Fact 9]. `BatchTranscriber` caches VRAM and VAD parameters from `TranscriberConfig` to enable dynamic batch sizing at runtime [Fact 1].

### 4. **OOM Handling & Resource Recovery**  
When CUDA Out-of-Memory (OOM) occurs, `ASRService.transcribe_raw` [asr::ASRService.transcribe_raw] explicitly clears caches and performs IPC cleanup before re-raising [Fact 2], and includes a `torch.cuda.empty_cache()` call specifically added on 2025-06-11 [Fact 15].

### 5. **Architecture & Integration**  
The modular architecture separates GPU work from CPU-bound tasks: `CPUWorkerPool` handles I/O, downloads, and pre-processing separately to avoid occupying GPU slots [Fact 18]. CUDA/CUDNN is used for GPU work management via `GPUWorkerPool` with stream-level concurrency control [Fact 7], while the broader system supports horizontal scaling with worker pools and gRPC inference routing [Fact 4].

In summary, the ASR service combines static allocation modeling, dynamic batch sizing, CUDA stream concurrency, blocking queue-based scheduling, and proactive OOM recovery to manage GPU resources robustly in production.
```

---

## Q20. What audio formats does the ASR service accept?

```

The ASR service accepts unstructured audio bytes with no explicit format specification in the `InferenceRequest` protocol message itself [Fact 15]. However, supporting services and components handle multiple audio formats:

- The `asr::AudioChunk` type supports NumPy arrays for legacy compatibility and represents mono audio slices with preserved sample rate and channel identity [Facts 4, 7]
- The `message::Decode` function accepts both `.wav` and `.mp3` formats when parsing JSON message bodies [Fact 2]
- The `util::inferExtensionFromContentType` function maps 6 audio MIME types to file extensions: `mp3`, `wav`, `flac`, `aac`, `m4a`, and `ogg` [Fact 3]
- The `vector-transcribe-audio-api` service converts uploaded audio files into standardized MP3 format using FFmpeg [Fact 9]
- The legacy `upload.php` endpoint accepts audio via uploaded files or base64-encoded POST data [Fact 8]
- The `owr.scripts::bulk_transcript_reprocess_from_json` function passes media type (`wav`/`mp3`) to determine file content type when uploading to the transcription service [Fact 1]

While the ASR service's protobuf definition does not restrict or specify audio format, the surrounding infrastructure clearly supports at least `wav`, `mp3`, `flac`, `aac`, `m4a`, and `ogg`, with conversion to a standardized format (MP3) happening upstream in the audio pipeline.
```

---

## Q21. How does Faster-Whisper compare to the standard Whisper model in this deployment?

```

Based on the retrieved knowledge, there is no direct comparison between Faster-Whisper and the standard Whisper model provided in the context.

However, we can infer the following:

- The `vector-transcribe-asr-api` service **uses Whisper** (explicitly mentioned in its description [Fact 1, 3]) as its core ASR engine, alongside Pyannote diarization.
- Faster-Whisper was **updated to v1.2.1** in this ecosystem on 2025-10-31 to support Silero V6 [Fact 1], suggesting it is now integrated somewhere in the stack — likely in place of or alongside standard Whisper.
- The `BatchTranscriber` class initializes two `WhisperModel` instances (primary and multilingual), wrapped in `BatchedInferencePipeline` [Fact 2], indicating a layered model setup — but it doesn’t specify whether these are standard Whisper or Faster-Whisper models.
- Configuration (`config.py`) references a multilingual Whisper model (`LANG_MODEL_NAME`, default `'small'`) used for language detection before processing with `ml_model_name` (default `'large-v3'`) [Fact 15], which aligns with standard Whisper naming conventions.

Since Faster-Whisper is known to be a drop-in, optimized alternative to Whisper (using int8 quantization and CUDA optimizations for faster inference), and its update was noted in the same environment where Whisper is used, it is plausible that Faster-Whisper is used *instead of* standard Whisper for improved performance. But the facts **do not explicitly confirm this** — only that Faster-Whisper was updated to support Silero V6, and that Whisper remains the base engine.

Therefore: **The deployment context does not specify how Faster-Whisper compares to standard Whisper — only that Faster-Whisper v1.2.1 is in use and integrated with Silero V6.**
```

---

## Q22. How does the QA scoring system work end-to-end?

```

The QA scoring system works through a multi-stage pipeline that processes transcripts, evaluates responses against scored questions, and generates analytics and reports. Here's how it works end-to-end:

---

### 1. **Ingestion and Transcript Preparation**
Scoring begins with media transcripts, which are processed by transcript services. For example, `owr.scripts::objTranscript` is responsible for loading, analyzing, and scoring media transcripts [Fact 1]. Transcripts can be reprocessed in bulk via `owr.scripts::bulk_transcript_reprocess_from_json` [Fact 17], or individually scored via `Transcript::scorecard()` [Fact 12].

---

### 2. **Scorecard Evaluation**
Scorecard evaluation is handled by `docroot.remote::scorecard_callback`, the core endpoint for automated scoring [Fact 8, Fact 9, Fact 13, Fact 19]. This endpoint:

- Validates input responses and matches them against expected scorecard answers [Fact 9].
- Splits responses into words and performs pattern matching, with special handling for `<output>...</output>` tagged sections [Fact 9].
- Supports flags like `auto_fail` and `bonus`, which adjust the grading mode and `points_possible` for questions [Fact 19].
- Triggers reruns for flagged answers (e.g., ambiguous or low-confidence matches) [Fact 8, Fact 13].
- Executes recursive scoring rules—additional scorecards are triggered when `recursive_pass_scorecards` or `recursive_fail_scorecards` are configured [Fact 13].

The system assigns scores per question, tracking raw values and percentages. For example, `docroot::report_agent_scorecard_questions` calculates per-question percentages and aggregates totals like `points_possible` and `points_scored` per agent [Fact 10].

---

### 3. **Score Aggregation and Analytics**
Once questions are scored, analytics and reporting subsystems compute higher-level insights:

- `docroot::GradedScorecardsReport` provides consistent totals and supports export requests for graded scorecards [Fact 4].
- `docroot::report_agent_scorecard_questions` computes agent-level performance, including per-question averages stored in `arrQuestionAverage` [Fact 7].
- `class.answer_distribution_report::AnswerDistributionReport` identifies coaching opportunities by pinpointing questions with the lowest average scores and detects common "N/A" answer patterns [Fact 14].

---

### 4. **Final Score Calculation**
Final scores are normalized to percentages. In `docroot::main`, the final score is computed using:
```
(scored - auto_fail_points_scored) / possible * 100
```
This accounts for auto-fail penalties and yields a standardized score [Fact 11].

---

### 5. **Workflow Orchestration**
Automated scoring tasks are orchestrated via message queues (e.g., AMQP), with `owr.scripts::process_automated_scorecards.php` as the entry point. It processes queue tasks using resilient connection logic and backoff strategies [Fact 2]. Priority for scoring tasks comes from company configuration, retrieved via `Session::getCompanyQueuePriority('auto_scorecard')` [Fact 12].

Retry logic is also implemented—e.g., `owr.scripts::bank_automation_retry.php` re-enqueues scoring tasks via `Transcript::scorecard()` [Fact 12].

---

### 6. **Observability and Testing**
The system is instrumented with metrics and health endpoints:
- `src::HealthServer` tracks job completion, requeues, and timing per `qa_type` (single, multi, summarize, unknown) [Fact 6].
- Tests use mocks like `serverhttp::mockAsync` to verify that LLM questions are correctly queued and transformed [Fact 3].
- Token counting logic is tested via `llm::TestCountTokens`, ensuring token estimates are within acceptable margins [Fact 5].

Together, these components form a resilient, scalable QA scoring pipeline from ingestion to analytics and reporting.
```

---

## Q23. What is the relationship between vector-transcribe-qa and llm-worker?

```

The `vector-transcribe-qa` and `vector-transcribe-llm-worker` services have a producer-consumer relationship in the LLM-powered transcription and question-answering (QA) pipeline:

- `vector-transcribe-qa` acts as the **job router and queue manager**. It receives AMQP jobs (e.g., from RabbitMQ), routes them based on token count and capacity constraints, stores job metadata in DocumentDB and MySQL, and dispatches them to the appropriate LLM worker queue [fact 9]. It supports dynamic configuration via AWS AppConfig and ensures idempotency and fault tolerance through job state management and atomic operations [fact 9].

- `vector-transcribe-llm-worker` acts as the **LLM executor**. It consumes jobs from the AMQP queue (specifically those routed by `vector-transcribe-qa`), processes them using a `VLLMClient` to call a vLLM-based model endpoint, and emits structured metrics and logs [fact 2]. It implements a `QAProcessor` that relies on a well-defined contract with `VLLMClient`, expecting responses in a specific format (`choices` → `message` → `content`) for chat-based QA or plain strings for summarization [fact 11].

In summary, `vector-transcribe-qa` handles job orchestration, scheduling, storage, and resilience, while `vector-transcribe-llm-worker` focuses on high-throughput, resilient LLM inference execution. Their contract is explicit on response format, and they share infrastructure elements like AMQP and DocumentDB [fact 9, 11, 14].
```

---

## Q24. What LLM inference engine does llm-worker use?

```

The `vector-transcribe-llm-worker` service uses **vLLM** as its LLM inference engine.

This is confirmed by Fact 1, which explicitly states:  
> **Integration point: vLLM (via OpenAI-compatible chat completions API) for LLM inference** [vector-transcribe-llm-worker]

Additionally, Fact 16 notes that the initial commit established the service with **vLLM for inference**, reinforcing this as the core LLM backend [vector-transcribe-llm-worker].
```

---

## Q25. How are QA scorecard prompts structured and evaluated?

```

QA scorecard prompts are structured and evaluated through a multi-step process involving transcript analysis, LLM integration, and callback-based validation. Here's how it works:

### Prompt Structure

1. **Multi-Prompt Support with Steering Keywords**  
   The `Transcript::scorecard()` method supports multi-prompt queries using *steering keywords* discovered from transcript content itself [Fact 2]. This implies prompts are dynamically generated based on the transcript's thematic elements.

2. **LLM Question-Answer Payload**  
   The underlying LLM question-answering system uses the `QuestionAnswer` type, which explicitly supports both single `Prompt` and array of `Prompts`, each optionally tagged with `QuestionWithComplexity` to indicate difficulty or priority levels [Fact 8]. This allows structured grading based on complexity.

3. **Job-Level Prompt/Question Pairing**  
   At the database level, the `Job` type contains paired `Question/Questions` and `Prompt/Prompts` fields, indicating support for multiple or progressive interactions within a single scoring task [Fact 13].

### Evaluation Process

1. **Scoring Delegation**  
   Scoring is triggered either via entry scripts like `process_automated_scorecards.php` or `bank_automation_retry.php`, both of which delegate to `Transcript::scorecard()` with queue priority derived from company configuration [Facts 3, 5].

2. **Callback-Based Validation**  
   The `scorecard_callback` endpoint receives LLM responses and validates them by:
   - Splitting responses into words and matching against expected scorecard answers [Fact 9]
   - Handling special `<output>...</output>` tagged sections [Fact 9]

3. **Recursive Scoring Rules**  
   The callback implements recursive scoring: when a grade is *Pass* or *Fail*, and if `recursive_pass_scorecards` or `recursive_fail_scorecards` are defined, additional scorecards are automatically triggered [Fact 19].

4. **Feedback Structuring**  
   Filter evaluation results are represented by `FilterVerdict`, which provides structured pass/fail outcomes with optional justification—suggesting a similar pattern may be used for scoring feedback [Fact 12].

5. **Statistical Aggregation**  
   `Agent::getScorecardStats()` counts total evaluations, pass counts, and fail counts for a given agent’s media entries—indicating scoring outcomes are systematically tracked [Fact 10].

In summary, scorecard prompts are context-aware, multi-prompt structures that feed into an LLM pipeline, with evaluations performed through callback-based validation, recursive scoring rules, and structured feedback mechanisms.
```

---

## Q26. How does the QA gateway route requests to the LLM worker?

```

The QA gateway routes requests to the LLM worker through a **dual-queue, token-aware routing mechanism** implemented in the `vector-transcribe-qa` service. Here's how it works:

1. **HTTP Ingestion**: The gateway exposes a `POST /v1/llm/qa` endpoint via the `serverhttp::New` function, which is handled by `handler.queueQuestionAnswer` [Fact 4, Fact 9].

2. **Token-Based Routing Decision**: When a QA request arrives, the `QueueLLMQuestions` method (implemented by `asyncapi::API.QueueLLMQuestions`) tokenizes the document and prompt to determine their combined length [Fact 10]. Based on this count, it routes the job to either `ProcessQueue` or `ProcessQueueBig`—this is the core of the “Dual-Queue Routing Based on Token Budget” architectural pattern [Fact 5].

3. **Priority and Persistence**: Jobs are saved to a database (specifically `jobDocument` with fields like `queue`, `tokens`, `priority`, etc. [Fact 6]), encrypted, and published to AMQP (e.g., RabbitMQ) with priority encoding. This ensures resilient delivery and capacity-aware scheduling [Fact 10].

4. **LLM Worker Consumption**: The `vector-transcribe-llm-worker` service consumes these AMQP messages via its `Worker` (an AMQP consumer) [Fact 11]. It delegates to a `QAProcessor`, which routes the request to `VLLMClient` methods based on the QA type defined in `src::QAType` [Fact 2, Fact 11].

5. **Asynchronous Dispatch**: The entire flow supports asynchronous callbacks—jobs include `callback_url` and `callback_data` fields so the worker can post results back to the requesting service once processing is complete [Fact 6, Fact 14].

This design enables isolation between small and large workloads, prevents resource starvation, and ensures scalable, fault-tolerant LLM processing [Fact 5, Fact 16].
```

---

## Q27. How does the encryption service implement envelope encryption with AWS KMS?

```

The `vector-encrypt` service implements envelope encryption using a layered cryptographic approach involving AWS KMS for key management and AES-GCM for data encryption [fact1, fact8].

Here's how it works:

1. **Data Key Generation**: When encrypting data, the service calls `kmsutil.GenerateDataKey` (which wraps `awsiface.KMSClient.GenerateDataKey`) to request a new data encryption key (DEK) from AWS KMS [fact5, fact9]. This returns both a plaintext DEK and a KMS-encrypted copy of the same key.

2. **Data Encryption**: The plaintext DEK is used locally with AES-GCM to encrypt the actual payload (e.g., JSON or file content) [fact1, fact8]. The `handlers::EncryptBytesToEnvelope` function, for instance, combines `kmsutil.GenerateDataKey` with `crypto.EncryptAESGCM` to produce an encrypted envelope [fact6].

3. **Envelope Construction**: The encrypted data (ciphertext) and metadata (including the KMS-encrypted DEK, IV, and AAD) are packaged into a `vectorencryption::Envelope` structure. This envelope is a JSON object with two base64-encoded fields: `data` (the ciphertext) and `metadata` (encryption parameters) [fact7, fact14].

4. **Storage/Transmission**: The envelope can be returned directly in API responses (for small payloads) or stored in AWS S3 as the body of an object (for large files via `handlers::EncryptFileS3`) [fact3, fact13].

5. **Decryption Workflow**: During decryption, `handlers::DecryptHandlerSmall` uses `kmsutil.DecryptDataKey` to call `KMS.Decrypt` and recover the plaintext DEK from the envelope’s metadata [fact11, fact17]. The DEK then decrypts the `data` field using AES-GCM.

6. **Optimization**: A Valkey-backed cache stores DEKs to reduce KMS calls, especially for repeated operations [fact1, fact8].

The entire process is implemented via well-defined interfaces (e.g., `awsiface.KMSClient`) and modular functions (e.g., `EncryptBytesToEnvelope`, `DecryptDataKey`), supporting both small JSON payloads and large file uploads with secure temp-file handling [fact8, fact4, fact16].
```

---

## Q28. What is the encrypt/decrypt flow for audio files in vector-encrypt?

```

The encrypt/decrypt flow for audio files in the vector-encrypt ecosystem follows a multi-layered, version-aware strategy that integrates with AWS S3, external encryption services, and fallback mechanisms. Here’s a detailed breakdown:

### Encryption Flow
1. **Encryption Service Integration**  
   Audio files are encrypted using the `vectorencrypt` service (via `VectorEncrypt::encryptFileS3` or similar methods). This is implemented in services like `s3client::EncryptedS3Client` in Go ([s3client::EncryptedS3Client](fact_2)), which integrates with `vectorencrypt.Client` for client-side encryption/decryption. In the PHP pipeline, the `asyncapi::API.QueueWavFile` function calls `encryptService.EncryptFileS3` to encrypt, upload, and publish audio files to S3 before triggering transcription jobs [asyncapi::API.QueueWavFile](fact_19)].

2. **Format Envelope**  
   Encrypted files are stored as *VectorEncrypt envelopes*—structured JSON wrappers. The `VectorEncrypt::isEnvelope` method validates whether a given payload is in this format ([VectorEncrypt::isEnvelope](fact_11), [fact_16]). This is part of the evolved encryption strategy introduced to replace legacy AES encryption ([vector-transcribe-ui](fact_15)).

### Decryption Flow
Decryption is handled contextually depending on where and how the file is accessed:

1. **Media Playback (Web Endpoint)**  
   The `agent-docroot::playback` endpoint fetches encrypted audio from S3, decrypts it using `Encrypt::decrypt`, and streams the decrypted WAV content with proper HTTP range and content-length headers [agent-docroot::playback](fact_1), [fact_3]). This indicates the decryption happens client-side or server-side before delivery.

2. **Reprocessing Workflow**  
   The `owr.scripts::processCompanyMedia` function decrypts audio using `Encrypt::decryptFileS3`, temporarily stores the plaintext in a secure system temp file (e.g., via `tempnam`), and processes it with `Media::start` [owr.scripts::processCompanyMedia](fact_7), [owr.scripts::auto_call_reprocess](fact_14)). This ensures plaintext never persists longer than needed.

3. **Transcription/Postback Systems**  
   Services like `vector-transcribe-postback` and `vector-transcribe-qa` decrypt payloads *before* processing. In `vector-transcribe-postback`, they consume AMQP messages containing S3 paths to encrypted transcripts/audio, then use a circuit-breaker-protected encryption service to decrypt the files [vector-transcribe-postback](fact_10)). Similarly, `vector-transcribe-qa` integrates decryption into message consumption [vector-transcribe-qa](fact_18)).

4. **Fallback & Legacy Handling**  
   In systems like `owr.scripts::main`, decryption strategies are prioritized and fall back gracefully:
   - Detect VectorEncrypt envelope format using `VectorEncrypt::isEnvelope`
   - Try JSON wrapper with `data` field
   - Try base64-decoded envelope
   - Detect legacy prefixes (`__E__`, `__EV2__`)
   - Use local `Encrypt::decrypt` as a fallback for non-envelope content [owr.scripts::main](fact_8), [Encrypt::decrypt](fact_9)).

5. **Waveform and UI Endpoints**  
   The `docroot::waveform` endpoint decrypts waveform JSON using `Encrypt::decrypt` and serves it as JSON [docroot::waveform](fact_5)].

### Temporal Evolution
The encryption strategy evolved to prioritize VectorEncrypt format as the primary envelope, with guards for legacy AES and double-encrypted content ([vector-transcribe-ui](fact_15)). `EncryptedS3Client` decodes envelopes into base64, converts to binary, and writes to local files for downstream processing ([s3client::EncryptedS3Client](fact_19)).

In summary, the audio file encrypt/decrypt flow is:
- **Encrypt**: Service calls `EncryptFileS3`, stores VectorEncrypt envelope on S3.
- **Decrypt**: Based on context (playback, reprocessing, postback), the system detects format, decrypts via `Encrypt::decrypt` or `vectorencrypt.Client`, and processes or streams the plaintext—using temp files securely and falling back to legacy strategies when needed.
```

---

## Q29. How do other services authenticate with the encryption service?

```

Based on the retrieved knowledge, **other services authenticate with the encryption service using service-to-service tokens** issued by an `auth-service`. Specifically:

- According to [Fact 19], service-to-service tokens are issued by `auth-service`’s `/internal/token` endpoint and are scoped to specific permissions [atlaskb::libraries::common-lib::auth (module)].

While the exact mechanism (e.g., how the token is included in requests—likely via an `Authorization: Bearer <token>` header) is not explicitly stated in the provided facts, the use of a dedicated token endpoint for scoped permissions is a standard pattern for mutual service authentication.

Additionally, some services are known to integrate directly with the encryption service:

- `vector-transcribe-llm-worker` uses `EncryptionClient` for prompt/response obfuscation [Fact 2].
- `vector-transcribe-intake` uses `encrypt::Client` to wrap/unwrap S3 objects [Fact 3].
- `vector-transcribe-redaction` and `vector-transcribe-postback` both use encryption clients with circuit breakers and retry logic for HTTP calls to the encryption service [Facts 13, 12].

However, none of these facts specify the *authentication header or mechanism* beyond referencing the client types and endpoints. The common-lib’s auth module strongly implies that token-based authentication is the standard approach across services.
```

---

## Q30. What happens if AWS KMS is unreachable?

```

If AWS KMS is unreachable, the behavior depends on the specific service and operation:

In the **vector-encrypt** service:
- The `EncryptBytesToEnvelope` function explicitly returns an error when KMS `GenerateDataKey` fails [handlers::EncryptBytesToEnvelope (function) [5]].
- The `encrypt` HTTP endpoint returns a 500 status code for KMS failures [handlers::encrypt (endpoint) [6]].
- Similarly, the `decrypt` endpoint returns 500 for KMS failures during decryption [handlers::decrypt (endpoint) [10]].

In the **vector-transcribe-recording-mover-v2** service:
- Errors during sidecar fetching (which may include KMS operations) are treated as fatal unless metadata is optional, potentially causing job drops even in transient network scenarios [vector-transcribe-recording-mover-v2 (service) [19]].

In the PHP-based **aws::Aws** class:
- All AWS API calls (including KMS operations) are wrapped in try-catch blocks to handle AwsException gracefully [class.aws::Aws (type) [14]], though the specific recovery behavior isn't detailed.

Configuration loading also depends on KMS:
- The `config::Manager.establishSession` function returns an error if any AWS call fails or required configuration tokens are missing, which would include failures reaching KMS if needed for token validation [config::Manager.establishSession (function) [2]].

The vector-encrypt service acknowledges that AWS AppConfig dependence introduces startup latency if configuration is missing or unreachable, though this specifically relates to AppConfig, not KMS directly [vector-encrypt (service) [4]].
```

---

## Q31. How does the postback service deliver transcription results?

```

The postback service delivers transcription results by sending **HTTP callbacks** to user-provided endpoints. Based on the entity summary for `vector-transcribe-postback`, after consuming jobs from an AMQP queue and processing payloads (which include S3 paths to encrypted transcripts and optional audio), the service:

- Decrypts files using a circuit-breaker-protected encryption service,
- Sends HTTP callbacks to user-provided endpoints,
- Implements **SSRF protection** with DNS-time IP validation,
- Supports both **multipart and JSON formats** for the callback payloads.

This delivery mechanism is confirmed by Fact 1 and Fact 10, both citing the `vector-transcribe-postback` service as an HTTP Callback Receiver validated for SSRF safety. Additionally, `Media::handlePostback` (Fact 2) and `Transcript::store` (Fact 11) describe how the postback JSON payload is processed and persisted, completing the delivery and storage lifecycle.
```

---

## Q32. What retry logic does postback use for failed deliveries?

```

Based on the retrieved knowledge, postback handling for failed deliveries does **not** appear to implement retry logic for the callback delivery itself. Here’s what is known:

- The `asyncapi::API.handleDelivery` function [Fact 15] completes a job in the database after performing the callback, but **still acknowledges the message even if the database update fails**. This indicates that the callback is treated as a final step, and failure to update the database post-callback does not trigger a retry of the callback.

- The `asyncapi::API.callback` function [Fact 3] logs jobID and callbackURL when an error occurs if the postback returns false, but the fact only describes logging behavior on failure—not retrying.

- The `asyncapi::callbackClient` [Fact 20] is described as an interface abstracting HTTP callback clients (e.g., `httpclient.Client`), but no retry-specific behavior is associated with it in the provided facts.

- In contrast, other components like `router::EventRouter` [Facts 1, 4, 18] and `owr.scripts::main` [Facts 2, 19] explicitly implement retry logic (e.g., exponential backoff, retry queues, attempt counting), but these are unrelated to the postback flow.

Thus, the available facts suggest that **the postback mechanism itself does not implement retry logic for failed callback deliveries**; it appears to be a one-shot attempt with error logging, but not requeuing or retrying.
```

---

## Q33. What formats can postback deliver results in?

```

Postback can deliver results in two formats, as demonstrated by the `asyncapi.httpclient::Client.NewRequest` function:

1. **Standard format**: Uses multipart content with `callback_data` and `transcript` fields [10].
2. **JSON format**: Uses multipart content with a `json` field [10].

These formats are used when sending HTTP POST requests for postback callbacks, allowing flexibility in how the response payload is structured.
```

---

## Q34. How does the audio API convert recordings to MP3?

```

The audio API converts recordings to MP3 through a layered architecture with two primary conversion paths:

1. **Local Conversion with FFmpeg**: The `class.audio::Audio` class provides audio-to-MP3 conversion using either a remote service or **local FFmpeg** [Fact 2]. When using local conversion, the `class.audio::Audio.convertLocally` function opens the audio file using the FFmpeg library, configures MP3 output format, and saves the result to disk [Fact 8].

2. **Service-Level Orchestration**: The `service.audio::Service` interface defines a `ConvertAudio` method that orchestrates the conversion workflow—validating the request, generating UUIDs, logging start/end events, and delegating to the underlying converter [Fact 5, Fact 16]. The actual conversion is delegated to `s.converter.Convert()` with a wrapped `ConvertRequest` [Fact 16].

3. **HTTP Endpoint Handling**: The `http.handler::AudioHandler` manages the full lifecycle of audio conversion HTTP requests—including file upload, processing, and response streaming [Fact 3]. Legacy endpoints like `docroot.api-legacy::upload.php` accept audio via uploaded files (`$_FILES['audio']`) or base64-encoded POST data [Fact 15].

4. **API Contract Enforcement**: The `vector-transcribe-audio-api` enforces a contract where `audio::FFmpegConverter` implements the `AudioConverter` interface, guaranteeing it accepts `context` and `ConvertRequest` and returns `ConvertResult` or error [Fact 11].

5. **Web Client Integration**: On the client side (e.g., in `docroot.medical::initFunction.handlerFunction`), audio streams are captured using `MediaRecorder`, which emits data chunks that are pushed into an `audioChunks` array for later upload [Fact 14].

The system supports both `.wav` and `.mp3` input formats, with a fallback mechanism (`src::_try_as_mp3`) that renames `.wav` files to `.mp3` when bitrate detection fails, suggesting some WAV files are mis-detected as MP3 initially [Fact 19].
```

---

## Q35. What audio processing tools or libraries does vector-transcribe-audio-api use?

```

The `vector-transcribe-audio-api` service uses several audio processing tools and libraries, based on the retrieved facts:

- **soundfile (libsndfile)**: Used in `utils/_decode_to_pcm16` for decoding audio to 16-bit mono PCM at 16kHz, and also for multi-channel to mono conversion [fact 3]. It is also used in `utils/_duration_via_soundfile` to calculate audio duration via `sf.SoundFile` [fact 15].

- **NumPy**: Used extensively for audio array processing, including resampling and normalization, particularly in integration with the ASR pipeline (e.g., in `AudioPreprocessor`) [fact 1]. `AudioChunk` provides NumPy compatibility via its `.audio` property for legacy service paths [fact 12].

- **FFmpeg**: Used as a fallback for audio decoding in `utils/_decode_to_pcm16` when `soundfile` fails [fact 3]. Also referenced in `class::Audio` in a related PHP component for MP3 conversion, indicating FFmpeg is a common external tool in the ecosystem [fact 16].

- **External tools via subprocess**: While not explicitly confirmed *in* `vector-transcribe-audio-api`, the closely related `vector-transcribe-redaction` service uses `soxi` (from sox) for audio bitrate detection, suggesting similar external tooling may be present in the broader audio pipeline [fact 7].

- **Web Audio API (OfflineAudioContext)**: Appears in `agent-docroot.js`, suggesting frontend or auxiliary tools use it for decoding and analyzing audio buffers, though this is not part of the core backend API [fact 4].

No direct mention of libraries like `pydub`, `librosa`, or `torch`/`torchaudio` is found in the provided context for this specific service—only NumPy is explicitly cited for audio array operations [fact 1].

Summary: `vector-transcribe-audio-api` relies primarily on **soundfile**, **FFmpeg (fallback)**, and **NumPy** for decoding, resampling, normalization, and array processing.
```

---

## Q36. What is the full message processing pipeline in the redaction service?

```

Based on the retrieved facts, the redaction service (`vector-transcribe-redaction`) implements a **Redaction Pipeline with Caching and Rule Prioritization** as its core architectural pattern [vector-transcribe-redaction]. This pipeline is implemented as a multi-stage, state-mutating sequence:

1. **`apply_replacements`**
2. **`check_repeats`**
3. **`silence calculation`**
4. **`redact_audio`**

Each stage consumes the output of the previous one [vector-transcribe-redaction].

Additionally, the service handles the full lifecycle of transcription jobs, which includes:

- Receiving encrypted AMQP messages [vector-transcribe-redaction]
- Decrypting transcripts and audio files
- Applying company-specific redaction rules, which involve:
  - Pattern-based replacements
  - PII (Personally Identifiable Information) detection
- Silencing redacted segments in the audio file using the `[redacted]` marker to identify redacted words [src::redact_audio, tests::test_run_all_redacted]
- Re-encrypting and uploading artifacts

The service also relies on caching in the Database to avoid redundant lookups for companies and replacement rules, and it applies rules in an ordered, prioritized manner. Rule precedence is guided by constraints such as `Company.large_number_min` for thresholding and `overwrite_recognized_entities` to determine whether manually defined redactions override automatically recognized ones [vector-transcribe-redaction].

The external dependency `vector_redaction_service` contributes modular components including pipeline construction and redaction logic, with a `simple_redactor` module specifically handling the transformation of a transcript into a list of redacted word dictionaries [vector_redaction_service].
```

---

## Q37. How does the PII detection pipeline work and what PII types does it detect?

```

The PII detection pipeline in the `vector-transcribe-redaction` service uses the **`pipelines.pipeline.get_full_identification_pipeline()`** function exposed by `vector_redaction_service`, which is built once at startup for cloning per request [Fact 1]. This pipeline integrates with **Presidio analyzer** for PII detection [Fact 3].

Specifically, the `serverhttp::SimpleHTTPRequestHandler` applies two regex patterns—`pattern_numcrap` and `pattern_nummany`—before invoking the Presidio analyzer, indicating the system first filters numeric patterns before performing broader PII detection [Fact 3].

The test suite (`TestRepeatedNumberPropagation`) validates PII number detection propagation through pipeline stages, including fuzzy matching behavior [Fact 2], suggesting numeric patterns—such as repeated or near-matching numbers—are among the detected PII types.

While the exact PII entity types detected by Presidio are not explicitly listed in the provided facts, Presidio is a well-known Microsoft library that typically detects:

- **Named entities**: persons, organizations, locations
- **Financial data**: credit card numbers, bank account numbers
- **IDs**: government IDs, passport numbers
- **Communication data**: email addresses, phone numbers
- **Dates and times**
- **Numeric patterns**: especially recurring or structured numbers (validated by the fuzzy matching tests [Fact 2])

Additionally, the service applies **company-specific redaction rules**, which may extend beyond standard PII categories [Fact 17].

In summary, the pipeline combines regex pre-filtering (for numeric patterns) and Presidio-based structured PII detection, with extensibility for custom redaction logic. The system ensures robustness through parallelized processing (using `ProcessPoolExecutor` for CPU-bound sklearn operations) and includes testing for stage-level propagation of detection logic [Fact 17, Fact 2].
```

---

## Q38. How does the redaction service handle word/phrase replacements?

```

The redaction service handles word/phrase replacements through a multi-step pipeline that involves transformation, pattern matching, and in-place mutation of transcript structures.

Here's how it works:

1. **Pipeline Execution**: The core logic resides in `src::_run_pipeline` [Fact 11], which marks words for redaction based on their `redacted` attribute (specifically when `redacted == 1`). This function orchestrates the pipeline transformation and simple redaction before converting results to Word objects with redaction masking.

2. **Redaction Triggering and Blocking**: The `src::Redaction` type [Fact 7] defines redaction behavior using *trigger words* (which initiate redaction ranges) and *blocked terms* (which prevent redaction within those ranges). This provides fine-grained control over what gets redacted.

3. **Replacement Contract**: According to the `vector-transcribe-redaction` service contract [Fact 3], the `apply_replacements` function is expected to mutate `transcript.words`, `transcript.transcript`, and `transcript.transcript_channels` *in-place* in a deterministic order defined by replacement rules.

4. **Punctuation Normalization**: Before applying redaction regex matching, `src::clean_word` [Fact 10] strips common punctuation (`.`, `;`, `:`, `!`, `?`) from words to normalize input and reduce false negatives.

5. **Word Replacement Logic**: The `vector_redaction_service` [Fact 19] includes a `redactors.simple_redactor` module that takes a transformed transcript object and returns a list of redacted word dictionaries. This indicates the replacement process operates on individual words and produces structured output.

6. **Testing Coverage**: Multiple tests validate different replacement scenarios:
   - Partial redaction where only `redacted=1` words become `[redacted]` [Facts 1, 8]
   - Complete redaction where all words are masked [Fact 2]
   - Preservation of `redacted=0` words unchanged [Fact 5]
   - Handling empty input [Fact 12]

7. **Configuration**: The service uses `Company.large_number_min` for thresholding and `overwrite_recognized_entities` to determine precedence between manually defined vs automatically recognized redactions [Fact 9], indicating replacements can come from multiple sources with configurable priority.

In practice, when a word or phrase matches a redaction rule, it gets replaced with the literal string `[redacted]` (as defined in `src::redact_audio`'s `REDACTED_TEXT` constant [Fact 6]), and the audio segments corresponding to these redacted words are silenced in subsequent processing steps [Facts 6, 16].
```

---

## Q39. What is the concurrency model in the redaction service - how does it combine asyncio with CPU-bound PII detection?

```

The redaction service (vector-transcribe-redaction) uses a hybrid concurrency model that combines asyncio for I/O-bound operations with ProcessPoolExecutor for CPU-bound PII detection.

Specifically, the service leverages **asyncio** as its core async execution engine for managing the 24-step redaction pipeline, including AMQP message consumption, file decryption, and orchestration of the overall workflow [vector-transcribe-redaction (service)], using `asyncio.wait_for` for timeout-bound operations and `asyncio.gather` for batch execution [atlaskb-python-test-repo (service)].

For CPU-bound PII redaction tasks (redaction_mod.run), the service uses **ProcessPoolExecutor** to parallelize operations across multiple processes [src::Worker] [vector-transcribe-redaction (service)]. This pattern was explicitly added on 2026-02-12 to handle sklearn-based PII detection workloads [vector-transcribe-redaction (service)]. The concurrency level is aligned with AMQP prefetch_count, defaulting to 5 [vector-transcribe-redaction (service)].

In the Worker implementation (src/consumer.py), each message is processed in its own asyncio task and ACKed/NACKed independently, allowing concurrent processing of multiple messages while delegating CPU-intensive PII detection to separate processes [src::Worker]. This architecture ensures that the event loop remains responsive to I/O operations while CPU-bound tasks are offloaded to worker processes.

The concurrency model follows an event-driven, async-first pattern with signal handling (SIGTERM/SIGINT) for graceful shutdown [vector-transcribe-redaction (service)], maintaining responsiveness during both startup and shutdown phases.
```

---

## Q40. How does the ProcessPoolExecutor interact with the asyncio event loop in the redaction service?

```

In the redaction service (`vector-transcribe-redaction`), `ProcessPoolExecutor` is used specifically to offload **CPU-bound PII redaction** work (`redaction_mod.run`) to parallel processes [src::Worker, Fact 1]. However, the provided facts **do not describe the specific mechanism** by which the `ProcessPoolExecutor` integrates with the asyncio event loop — for instance, whether `loop.run_in_executor()` is used or how the subprocess boundaries are managed.

What we *do* know from Fact 4 is that `ProcessPoolExecutor` is recognized as introducing overhead and complexity, and is *not ideal* for I/O-bound workloads — implying it is intentionally reserved for CPU-bound tasks to avoid blocking the event loop.

Additionally, the test class `tests::TestStop` [Fact 7] shows that the process pool is shut down using `run_in_executor` with a `None` executor (which defaults to the default executor pool) and a `shutdown` callable — indicating that *shutdown is awaited* via the event loop, but this is specific to teardown, not runtime task submission.

In summary:
- The `ProcessPoolExecutor` is used to prevent CPU-bound PII redaction from stalling the async event loop [src::Worker, Fact 1].
- It is likely submitted to via `loop.run_in_executor()` (standard pattern), but this is not explicitly stated in the provided context.
- Shutdown is explicitly managed via `run_in_executor(None, shutdown_fn)` during cleanup [Fact 7].

So while the *purpose* of using `ProcessPoolExecutor` is clear, the *runtime integration pattern* with asyncio is not fully specified in the retrieved knowledge.
```

---

## Q41. What happens when the process pool crashes with BrokenProcessPool in the redaction service?

```

When the process pool crashes with `BrokenProcessPool` in the redaction service, the service initiates a recovery sequence that involves ** pausing, probing, and re-registering the dependency**—a pattern referred to as *unified dependency recovery* [Fact 19].

This behavior is part of the broader error-handling strategy of the `vector-transcribe-redaction` service, which avoids job-level crashes during redaction logic by implementing structured dependency recovery [Fact 19]. Although the exact `BrokenProcessPool` handling is not explicitly detailed in the retrieved facts, it is reasonable to infer that it falls under this unified recovery pattern, given the explicit mention of `BrokenProcessPool`-type failures and the service's emphasis on resilience.

Additionally, the `Worker` component — the core async AMQP consumer responsible for CPU-bound PII redaction via `ProcessPoolExecutor` [Fact 1] — is designed to handle critical failures gracefully. It implements circuit-breaking, watchdog mechanisms, and health monitoring (e.g., setting `health.pipeline_ready(False)` on pipeline init failure [Fact 8, Fact 18]), suggesting that a `BrokenProcessPool` would likely result in the worker pausing operations, reporting unhealthy status, and initiating the re-registration flow upon recovery.

No evidence was found of explicit recovery logic *inside* the `redact` logic itself (e.g., in `check_repeats`), reinforcing that failures like `BrokenProcessPool` are handled at a higher coordination layer (e.g., `Worker`, `main`, or health infrastructure), not at the task level [Fact 19].
```

---

## Q42. How does the encryption circuit breaker work in the redaction service - what are its thresholds and recovery?

```

Based on the retrieved knowledge, the encryption circuit breaker implementation details differ between the two main services:

### In `vector-transcribe-postback` (Go service):
The circuit breaker is explicitly implemented with:
- **State management**: Uses atomic types (`atomic.Bool`, `atomic.Int32`, `atomic.Int64`) for thread-safe circuit breaker state without mutexes [Fact 6].
- **Threshold-based opening**: The circuit breaker opens after `encCircuitThreshold` failures [Fact 10].
- **State transitions**: 
  - Starts in closed (allow) state by default [Fact 3]
  - Remains closed when failure count is below threshold [Fact 18]
  - Transitions to half-open after opening, then closes after successful request [Fact 15]
- **Error recording**: Uses `encCircuitRecordFailure()` and `encCircuitRecordSuccess()` methods to track failures/successes [Fact 4, Fact 11].
- **Config change handling**: Resets both encryption client and circuit breaker state when service URL or key changes [Fact 5].
- **Error classification**: Errors containing 'encrypt service circuit open' are classified as 'encrypt_circuit_open' [Fact 9].

### In `vector-transcribe-redaction` (Python service):
The documentation is less specific about circuit breaker thresholds and recovery mechanisms:
- Uses circuit breaker logic with retries for encryption/decryption HTTP calls [Fact 1].
- Employs `retry_request` with jittered exponential backoff and `reset_circuit_breaker` for recovery [Fact 1].
- Integration with encryption service includes circuit breaker protection [Fact 7].
- Handles `EncryptionError` by marking encryption disconnected and requeuing [Fact 14].

**Key limitation**: While both services implement circuit breakers for encryption service interactions, the retrieved knowledge provides detailed implementation specifics (thresholds, state transitions, recovery logic) only for the `vector-transcribe-postback` service, not for `vector-transcribe-redaction`. The Python service mentions circuit breaker usage but lacks concrete thresholds, failure counts, or recovery timing details.
```

---

## Q43. What is the dependency recovery mechanism and how does consumer pausing work in the redaction service?

```

In the **vector-transcribe-redaction** service, dependency recovery and consumer pausing are handled through a **unified pause/probe/re-register pattern** [error_handling convention, Fact 1].

### Dependency Recovery Mechanism

- The service uses **custom exceptions** for specific database, encryption, and connection errors (e.g., `EncryptionError`, `DatabaseConnectionError`, `DocumentDBConnectionError`) to distinguish failure modes [Fact 16].
- Failures trigger a **pause-and-recover** workflow rather than crashing the job or consumer. This is reinforced by the design choice to **not raise exceptions in redaction logic** (e.g., `check_repeats`) to avoid job-level crashes [Fact 1].
- Consumer recovery was generalized on **2026-02-13** to handle failures across MySQL, DocumentDB, and encryption service dependencies using a single, unified mechanism [Fact 2].
- When recovery is needed, the `Worker` type uses **exponential backoff probe loops** (`pause_for_recovery`) to reconnect to failed services before resuming processing [Fact 11].

### Consumer Pausing Behavior

- Consumer pausing is implemented in the `Worker._drain` method and related logic, with tests like `TestDrain` handling consumer cancel timeouts and exceptions by safely setting `consumer_tag` to `None` to ensure graceful termination [Fact 9].
- A dedicated test (`TestRestartConsumers_NilCancel`) confirms that `restartConsumers()` gracefully handles nil `consumerCancel`, avoiding panics when trying to cancel a non-existent context [Fact 10].
- Pausing is not only for recovery — it is part of a broader lifecycle management that includes graceful shutdown, reconfiguration, and external dependency failures.

### Summary

The dependency recovery mechanism in `vector-transcribe-redaction` centers around **structured error classification**, **consumer pausing**, and **exponential backoff-based reconnection attempts**, allowing the service to recover from transient failures without crashing jobs or losing message processing continuity.
```

---

## Q44. How does the AMQP connection watchdog detect and recover from stale connections?

```

The AMQP connection watchdog detects and recovers from stale connections through a combination of health checks, explicit disconnection detection, and automatic reconnection with exponential backoff.

### Detection of Stale Connections

The **Worker** class in `src/consumer.py` implements an AMQP watchdog that actively monitors connection and channel health, broker blocked state, and consumer cancellation status [src::Worker, Fact 3]. It uses a **watchdog loop with exponential backoff** to detect and force-reconnect on stale connections [src::Worker, Fact 1].

Additionally, in the `vector-transcribe-llm-worker` service, the AMQP consumer detection logic evolved to use direct polling of `aiormq channel.consumers` dict and fallback watchdog logic that tracks `_last_message_time` and in-flight message tracking [Fact 15].

In the Go codebase, the `asyncapi::API` type manages a dedicated goroutine that watches the AMQP connection and closes the `connLost` channel upon connection failure [asyncapi::API, Fact 7]. The `ConnLost()` function returns a channel that is closed precisely when the AMQP connection drops, serving as the core mechanism for detecting unexpected disconnections [asyncapi::API.ConnLost, Facts 6, 16].

### Recovery Mechanisms

Recovery from stale connections involves several layered strategies:

1. **Exponential Backoff Reconnection**:
   - The `amqpConnectionManager` type implements exponential backoff ranging from 2 seconds to 1 minute by default [Fact 13].
   - The `Worker` uses exponential backoff in its watchdog loop for reconnection attempts [src::Worker, Fact 1].
   - In PHP scripts like `bank_automation_retry.php`, a `while(true)` loop with exponential backoff up to 60 seconds handles AMQP connection failures [owr.scripts::main, Fact 11], sleeping with exponential backoff on any `Throwable` during AMQP operations [Fact 19].

2. **Channel Recycling and Health Monitoring**:
   - The `vector-transcribe-qa` service uses a pattern where channel lifecycle is managed with `NotifyClose`, self-reset on closure (via `getPublishChannel`), and idle timeout detection (via `consumeCompleteQueue`) to maintain long-running worker health [Fact 12].
   - In PHP, `getAmqpChannel` implements lazy reconnect logic by checking connection and channel health before reuse, handling reconnection and cleanup of stale connections [owr.scripts::getAmqpChannel, Fact 4].

3. **Connection Configuration**:
   - The `process_automated_scorecards.php` function configures `AMQPStreamConnection` with `heartbeat=30` and tuned timeouts to avoid stale connections [Fact 5].
   - The `vector-transcribe-redaction` service removed the AMQP idle timeout on 2026-02-13, relying instead on heartbeat and watchdog health checks for stale connection detection [Fact 10].
   - The `vector-transcribe-ui` service included heartbeat-aware reconnect logic and removed obsolete config (`amqpalt`) to improve message broker resilience [Fact 8].

### Health Integration

The `HealthServer` tracks forced AMQP reconnections triggered by the watchdog, updating health state accordingly [src::HealthServer, Fact 2]. For instance, the `TestConnectionCallbacks` test class verifies that `_on_reconnected` calls `health.set_amqp_connected(True)` when the connection is restored [Fact 18].

In summary, the watchdog combines active monitoring (channel/consumer health, message timing), passive disconnection signals (`ConnLost` channel), and resilient recovery (exponential backoff, channel recycling, health-aware updates) to detect and recover from stale AMQP connections.
```

---

## Q45. What Prometheus metrics does the redaction service expose?

```

The redaction service (`vector-transcribe-redaction`) exposes Prometheus metrics via an internal metrics server that provides endpoints for `/health`, `/metrics`, `/live`, and `/ready` [vector-transcribe-redaction (service)].

While the service is explicitly noted to include *“production-grade observability through structured logging and Prometheus metrics”* [vector-transcribe-redaction (service)], the retrieved facts do **not** specify the exact metric names, types, or labels exposed by the redaction service’s `/metrics` endpoint — unlike other services such as `vector-transcribe-llm-worker` (which added 7 Prometheus metrics including counters for processing, encryption, LLM requests, and a latency histogram [vector-transcribe-llm-worker (service)]) or `vector-transcribe-qa` (which has windowed/lifetime counters and instrumentation on asyncapi paths [vector-transcribe-qa (service)]).

The closer reference to metric content comes from the Go-based `metrics::Metrics` type [metrics::Metrics (type)], which:
- Exports Prometheus metrics including uptime, in-flight count, completed/requeued totals, requeue reasons, watchdog reconnects, and timing histograms [metrics::Metrics (type)],
- Uses HELP and TYPE comments in Prometheus text exposition format [metrics::Metrics (type)],
- Includes both gauge and counter types in output [metrics::Metrics (type)].

However, since `vector-transcribe-redaction` is a Python service and no specific metrics definitions are tied to it in the facts, we cannot definitively list the *exact* metrics it exposes beyond the presence of a `/metrics` endpoint integrated with Prometheus.

**In summary**: The redaction service exposes Prometheus metrics via an internal HTTP endpoint, but the specific metrics (names, labels, types) are not detailed in the available context.
```

---

## Q46. What are the health check endpoints in the redaction service and what does each verify?

```

The redaction service exposes two primary health check endpoints:

1. **`/health` endpoint**  
   - **What it verifies**: This endpoint performs component-level health checks for multiple services including AMQP, encryption, database, DocumentDB, redaction pipeline, and consumer components. Each component returns its individual status (pass/fail), and the overall response includes a summary status, in-flight job count, concurrency settings, uptime, and job counters.  
   - **Authentication**: Publicly accessible—no authentication required [handlers_test::TestSecurity_HealthNoAuth].  
   - **Implementation**: Computed via `_overall_status()` and `_checks()` methods in the `HealthServer` class [src::HealthServer.health, src::HealthServer].

2. **`/health/ready` (readiness) endpoint**  
   - **What it verifies**: This endpoint checks if all *required* components—AMQP, encryption, database, and redaction pipeline—are ready and healthy. It returns HTTP 200 only when all required components are ready; otherwise, it returns HTTP 503. Unlike `/health`, its response code changes based on readiness, not just status reporting.  
   - **Implementation**: Shares the same underlying logic (`_overall_status()` and `_checks()`) as `/health`, but varies HTTP response codes depending on overall health [src::HealthServer.ready].

Both endpoints were added on **2024-03-20** as part of the health check and metrics infrastructure [pkg/api/health.go].

In addition, the encryption service participates in health checks by exposing an `/encrypt` endpoint used for probing—sent with empty data for lightweight health validation [src::EncryptionClient.probe]. The health flags themselves (e.g., `db_connected`, `encryption_connected`, `pipeline_ready`, `documentdb_connected`) are set during startup in a defined order during service initialization [tests::TestHealthStateDuringStartup].
```

---

## Q47. How does AWS AppConfig hot-reload work for the redaction service?

```

The provided knowledge base does not contain specific information about a **redaction service** or how AWS AppConfig hot-reload works for it.

While several services are described as using AWS AppConfig with hot-reload (e.g., `vector-encrypt`, `vector-transcribe-qa`, `vector-transcribe-intake`, `vector-transcribe-postback`, `vector-transcribe-audio-api`), none are explicitly named or described as a *redaction* service.

One relevant fact ([#13](#Fact13)) mentions a `src::Worker` type in `src/consumer.py` that performs hot-reload of configuration by draining in-flight messages, closing connections, applying new config, and re-registering consumers — this *could* be part of a redaction pipeline — but it’s not explicitly tied to a redaction service or confirmed to use AWS AppConfig.

Therefore, based on the provided facts, **we cannot determine how AWS AppConfig hot-reload works for the redaction service** — the service is not referenced, and no mechanism is described in context.

If you can clarify or provide additional context about which service is the redaction service (e.g., if it’s part of `vector-transcribe-intake`, `vector-transcribe-qa`, or another repo), I can revisit this with more precision.
```

---

## Q48. Which configuration keys can be changed at runtime vs requiring a drain-and-reconfigure in the redaction service?

```

Based on the retrieved knowledge, here's what we can determine about runtime vs drain-reconfigure configuration updates in the redaction service:

### Runtime-applicable changes (instant hot-reload):
1. **Encryption service credentials**: 
   - The `encrypt::DynamicClient` hot-reloads encryption service configuration by comparing `activeURL` and `activeKey` fields with config values before recreating the client [Fact 18].
   - When encryption config changes, `asyncapi::API.UpdateConfig` resets the encryption client [Fact 9].
   - Tests verify that `UpdateConfig` recreates `encryptClient` when encryption service credentials change [Fact 10].

2. **General application configuration**: 
   - `appconfigwatch::Start` applies atomic configuration updates by calling `runtimecfg.Set` with the new config [Fact 4].
   - The `testutil::SetupTestRouter` uses `runtimecfg.Set` to expose AppConfig globally so handlers can look up tenant config by API key [Fact 1].

### Drain-and-reconfigure required:
1. **Infrastructure keys**: 
   - The `src::Worker` explicitly implements hot-reload for runtime keys (instant) and infrastructure keys (drain → reconfigure → resume) [Fact 2].

2. **Caching configuration**: 
   - The `cache::Store` has a `Reconfigure` method that can disable caching by setting `cfg.Enabled` to false [Fact 11], though it's not explicitly stated whether this requires a drain.

The distinction appears to be that encryption-related configuration changes (URL, API key) and general application configuration can be applied instantly, while infrastructure-level changes require draining existing work before reconfiguration.
```

---

## Q49. What is the reconfigure sequence when an infrastructure key changes in the redaction service?

```

When an infrastructure key changes in the redaction service, the reconfigure sequence follows a **drain → reconfigure → resume** pattern.

Specifically, the `src::Worker` type performs hot-reload of infrastructure keys by:

1. **Draining in-flight messages** — completing or canceling current processing to avoid data loss or inconsistency.
2. **Closing connections** — terminating existing connections (e.g., AMQP) that depend on old infrastructure settings.
3. **Applying new config** — updating configuration fields (e.g., AMQP, encryption, logging) as determined by `src::AppConfigPoller`, which routes changes to `on_infra_change` when keys are in `src::INFRA_KEYS`.
4. **Re-registering consumers** — recreating consumers with the updated configuration and resuming message processing.

This sequence is explicitly described in [`src::Worker`](src/consumer.py) and validated in [`tests::TestAppConfigCallbacks`](tests/test_main.py), where the `on_infra_change` callback from `AppConfigPoller` forwards infrastructure changes to `worker.reconfigure()` [Fact 5, Fact 7].

This is distinct from runtime key changes, which are applied **instantly** without draining or connection restarts [Fact 6].
```

---

## Q50. How does the configuration priority chain work in the redaction service?

```

The redaction service uses a well-defined configuration priority chain that ensures configuration values are resolved in a predictable order.

In the main Python service (`vector-transcribe-redaction`), the `src::Config` class explicitly defines configuration precedence as:  
**init > env > AppConfig > INI file** [Fact 12].

This means configuration values are resolved in this order:
1. **init**: Programmatic initialization values (hardcoded or passed at startup)
2. **env**: Environment variables
3. **AppConfig**: AWS AppConfig remote configuration
4. **INI file**: Local INI-style configuration file

In contrast, the Go-based `mover` service (likely part of a related system) follows a different but conceptually similar precedence:  
**defaults → config.ini → environment variables → AppConfig remote → global overrides via applyGlobals** [Fact 15].

Additionally, the `Company` entity requires loading of queue priorities upon instantiation if `queue_priority` is empty, suggesting company-specific configuration may override or supplement service-level defaults [Fact 14].

Thus, while the exact chain differs between services (Python vs Go), both prioritize explicit/programmatic or environment-based overrides over file-based or remote configurations, ensuring flexible and deterministic configuration resolution.
```

---

## Q51. What is the INI section-to-field mapping convention in the redaction service?

```

The INI section-to-field mapping convention in the redaction service is defined by `_SECTION_PREFIX_OVERRIDES`, which maps specific INI section names to configuration key prefixes. According to [src::_SECTION_PREFIX_OVERRIDES (concept)](src/appconfig.py), the mapping is as follows:

- `encrypt` → `encryption`  
- `logging` → `log`  
- `redaction` → `redaction`  
- `worker` → `""` (empty prefix)

This means that configuration values in an INI section like `[redaction]` will be prefixed with `redaction_` when flattened into the final configuration dictionary [src::parse_ini_body (function)](src/appconfig.py).

In addition, the `parse_ini_body` function replaces dots in section names with underscores to match the naming convention of Config fields [src::parse_ini_body (function)](src/appconfig.py), and uses `configparser.ConfigParser` to parse INI content, flattening all sections into a single dictionary [src::parse_ini_body (function)](src/appconfig.py), [src::IniSettingsSource (type)](src/config.py).

Thus, for example, a key `threshold` in the INI `[redaction]` section would be transformed to `redaction_threshold` in the final config dictionary.
```

---

## Q52. How is the redaction service deployed with systemd?

```

The provided knowledge base does not contain any information about systemd deployment of the redaction service.

While several facts describe the service’s architecture, behavior, and internal logic—including its use of async processing, AMQP message consumption, configurable redaction pipelines, and health management—none mention systemd unit files, service configuration, or deployment mechanism. There is no reference to systemd in any of the cited entities or facts.

Therefore, based on the current context, it is not possible to determine how the redaction service is deployed with systemd.
```

---

## Q53. What external dependencies does the redaction service require?

```

The redaction service relies on several external dependencies, both as direct pip dependencies and via the broader service's go.mod:

1. **`vector_redaction_service`** — This is explicitly imported as a pip dependency and provides modular components including pipeline construction and redaction logic [Fact 1]. It is a core external pip module used for identification and redaction functionality.

2. **`github.com/aws/aws-sdk-go-v2/service/sso`** — Required at version **v1.30.3** as a dev/indirect dependency in the Go module [Fact 18]. This supports AWS Single Sign-On integration, likely for secure configuration or access management.

3. **`github.com/gabriel-vasile/mimetype`** — Required at version **v1.4.3** as a dev/indirect dependency in the Go module [Fact 20]. This is used for MIME type detection, likely when processing uploaded files.

4. **`typescript`** — Required at version **^5.3.3** as a dev/indirect dependency in package.json [Fact 19]. This suggests frontend UI tooling (e.g., for the redaction features UI) uses TypeScript.

Additionally, the service references UI-level frontend dependencies in `js.demo::anonymousFunctionf11b791f0100`, which uses **Bootstrap Wizard** and **BootstrapValidator** [Fact 10], although these are client-side and not runtime dependencies of the core redaction service.

No other external Python or Go dependencies are explicitly listed in the context as required by the redaction logic itself—only the above are directly tied to the service’s operation or development environment.
```

---

## Q54. How does the redaction service handle audio segment silencing?

```

The redaction service handles audio segment silencing through a dedicated **audio redaction pipeline** that operates after textual redaction. Specifically:

- The `src::redact_audio` function in `src/audio.py` performs audio redaction by invoking the **SoX (Sound eXchange) subprocess** with the input audio, output path, bitrate flag (`-C`), and padding commands to silence segments corresponding to redacted words in the transcript [fact 13].

- This step is part of the architectural pattern of the `vector-transcribe-redaction` service, where redaction is implemented as a **multi-stage pipeline**: `apply_replacements` → `check_repeats` → `silence calculation` → `redact_audio`. Each stage consumes the output of the previous, ensuring structured and ordered processing [fact 9].

- The silence calculation (`silence::calculate_silence`) identifies gaps > 3.0 seconds between consecutive words in a transcript and modifies `transcript.silence` in-place. However, this function alone computes total silence duration; it is the subsequent `redact_audio` function that physically silences segments in the audio file based on redaction ranges derived from the transcript [facts 4, 15].

- Redaction ranges are configured via the `src::Redaction` model, where trigger words initiate redaction ranges and blocked words prevent redaction within those ranges, ensuring precise targeting of sensitive segments [fact 20].

- The `src::Job` model includes a `skip_redaction` flag, allowing the transcription step to proceed while bypassing both textual and audio redaction if needed [fact 17].

- In the Go-based legacy scripts (e.g., `owr.scripts::processCompanyMedia`), prior results are cleared before resubmitting audio, and diarization is explicitly disabled, ensuring clean slate redaction workflows [fact 12].

Thus, audio silencing is handled via **SoX command-line invocation**, driven by redaction ranges inferred from the transcript, and embedded in a larger pipeline that ensures consistent, configurable redaction of sensitive speech segments.
```

---

## Q55. What is the recording mover processing pipeline from SQS to intake API?

```

The recording mover processing pipeline from SQS to the intake API follows a **message-driven workflow with decoupled stages** [Fact 4]. Here's the step-by-step flow:

1. **SQS Polling**: The service polls an AWS SQS queue (long-polling) for transcription job messages [Fact 3, Fact 6].

2. **Message Decoding**: Each SQS message is decoded. For S3 event messages, this involves extracting nested `S3EventRecord`, `S3EventDetail`, and `S3ObjectDetail` to precisely track source object metadata for download [Fact 14, Fact 6].

3. **Per-Company Configuration Loading**: A YAML config per company is loaded, supporting dynamic configuration via INI files, environment variables, and optionally AWS AppConfig [Fact 6, Fact 1].

4. **File Fetching**: Using the storage factory, the service fetches the media file from its source (S3, SFTP, or local filesystem) [Fact 1, Fact 6].

5. **Metadata Normalization**: The service normalizes metadata using compiled patterns and transforms (via `metadata::Service`) [Fact 6, Fact 1].

6. **Upload to Intake Endpoint**: Finally, the enriched recording is uploaded to the configurable intake endpoint [Fact 1, Fact 6].

The `handlers::IntakeHandler` handles the intake request workflow, including API key validation and orchestration of encryption and processing steps [Fact 5, Fact 9]. The entire pipeline is designed with layered architecture and dependency injection for testability and robust error handling [Fact 6, Fact 1].

This end-to-end pipeline ensures reproducibility through persisted state captured in `processing::Manifest`, which includes metadata, S3 keys, and retry information [Fact 7].
```

---

## Q56. How does multi-company mode work with dynamic AppConfig reconciliation in the recording mover?

```

Based on the retrieved facts, the recording mover service (`vector-transcribe-recording-mover-v2`) supports multi-company mode with dynamic AppConfig reconciliation through a **layered, hierarchical configuration resolution** model that merges local, environment-driven, and remote AppConfig sources.

### Configuration Resolution Flow  
The service follows a specific precedence order (Fact 14, 16):  
**defaults → config.ini → environment variables → AppConfig remote profile → global overrides (`applyGlobals`)**  

This layered approach enables flexible deployment across environments and supports dynamic updates without redeployment.

### Multi-Company Configuration Structure  
- Company-specific configuration is loaded as **per-company YAML** (Fact 16), suggesting a per-tenant or per-company context is supported.  
- The `CompanyConfig` type (Fact 15) defines fields like `MediaRetention`, `ScanDomain`, and `Timezone`, indicating that company-level policies are configurable and can be applied dynamically.  
- The contract between `CompanyConfig` and `metadata::Service` requires that `CompanyConfig` provides `MetadataConfig` and `SidecarConfig`, which must be valid and non-nil (Fact 13). This implies that each company’s configuration directly influences metadata processing behavior.

### AppConfig Integration  
- Configuration can be loaded from **AWS AppConfig**, either optionally or as the primary source, depending on runtime flags (e.g., `companyConfigSource == 'appconfig'`) (Fact 1).  
- A dedicated `AppConfigManager` type (Fact 17) coordinates local and remote (AppConfig) sources and supports **hot reloading** via a background polling loop and a reactive subscriber model.  
- The `loadFromAppConfigWithClient` function (Fact 20) enables direct AppConfig loading and returns the AppConfig client for reuse, supporting repeated pulls for dynamic updates.

### Dynamic Reconciliation in Multi-Company Mode  
While the facts do not describe an explicit per-company AppConfig polling mechanism, the architecture strongly suggests:
1. A **global AppConfig profile** may be pulled once and shared, or  
2. Each company may have its own AppConfig profile (e.g., per-tenant profiles), especially given that `vector-encrypt` (Fact 3) and `vector-transcribe-llm-worker` (Fact 6) both use AppConfig for *per-tenant settings* and *dynamic runtime configuration*.  

In `vector-transcribe-recording-mover-v2`, per-company YAML config is loaded per request or worker (Fact 16), and this local company config likely **overrides or supplements** global AppConfig values—consistent with Fact 2’s description of a hybrid local+remote merge pattern (local first, then merge with remote AppConfig when enabled).  

The presence of `applyGlobals` in the resolution chain (Fact 14) implies that AppConfig updates may propagate as global overrides across all tenants unless explicitly overridden by company-specific YAML.

### Summary  
Multi-company mode in the recording mover relies on:
- A **hierarchical config loading mechanism** that includes AppConfig as a high-priority remote source  
- Per-company YAML configuration layered atop global settings  
- A **hot-reload-capable AppConfig manager** enabling runtime updates  
- Company-specific policy enforcement (via `CompanyConfig`) that reacts to dynamic AppConfig changes  

While the facts imply strong AppConfig integration, they do *not* specify whether AppConfig is loaded **per company** or **globally with company overrides** — only that company config can be merged dynamically.
```

---

## Q57. What storage backends does the recording mover support?

```

The recording mover supports multiple storage backends, specifically:

- **S3** (Amazon Simple Storage Service)
- **SFTP** (Secure File Transfer Protocol)
- **Local filesystem**

This is confirmed by the `main` function in `cmd/mover/main.go`, which explicitly supports these three fetcher types for moving files [fact 12]. Additionally, the `FileLocation` message type supports a `Type` field that can indicate different storage types like `'s3'` and `'sftp'`, with conditionally relevant connection parameters [fact 4].

The `Processor.moveFilesOnError` function also indicates that it only moves files when `media.Type` is `'s3'`, and only if `p.Storage.HasMover(media.Type)` is true [fact 6], showing support for mover implementations per backend type.

Furthermore, the `Storage.Factory` maintains separate maps for fetchers and movers keyed by storage type, implying extensibility for new backends [fact 16].
```

---

## Q58. How does the mover extract metadata from filenames vs sidecar files vs embedded JSON?

```

The metadata extraction mechanisms differ based on the source—filenames, sidecar files, or embedded JSON—as evidenced by the following patterns in the codebase:

### 1. **Filename-based metadata extraction**  
Metadata is extracted from filenames using pattern-based matching:
- The `metadata::FilenamePattern` type [3] compiles regex patterns with **named capture groups** to extract key-value pairs directly from filenames. For example, a filename like `recording_2024-01-01_apikey_abc123.wav` could yield `{date: "2024-01-01", api_key: "abc123"}` if the pattern is configured accordingly [3].
- The `config::MetadataConfig` type supports a `filename` mode among other metadata sources (filename, sidecar, both, auto), indicating explicit control over this extraction strategy [1][4].

### 2. **Sidecar file extraction**  
Sidecar files are handled as auxiliary metadata files:
- The `simulate::collectSidecarFiles` function [7] collects sidecar data by identifying form fields named **exactly `'sidecar'`**, or prefixed with `'sidecar['` or `'sidecar_'`. This suggests sidecar data is transmitted as separate multipart fields alongside the main file.
- In `simulate::handler.processRecord` [17], sidecar files are matched to media files—first by index, and if that fails, by **base filename matching**. This ensures accurate pairing even when filename patterns vary.
- Sidecar metadata is likely parsed separately and then merged into the final payload during processing.

### 3. **Embedded JSON metadata**  
Embedded metadata comes in the form of JSON strings embedded within payloads:
- In `processor::simulatedUpload` [6] and `processor_test::uploadCapture` [10], metadata is passed in a **`'metadata'` field as a JSON string**, which is then parsed into `map[string]string`. This pattern appears consistently in both test and simulation contexts.
- The `media::Media` type in `docroot/remote/intent_callback.php` [8] and `sentiment_callback` [18] accesses metadata via `document['metadata']`, with null coalescing fallbacks, indicating the system expects metadata to be JSON-embedded in document structures.
- The AMQP consumer (`amqpconsumer::parseMetaFile`) [5] and related functions parse **newline-separated key=value files**, extracting metadata JSON and `job_id`, then merging the `job_id` into the metadata JSON object [13].

### Summary  
| Source       | Mechanism                                     | Key Entities Involved         |
|--------------|-----------------------------------------------|-------------------------------|
| Filename     | Regex-based named group extraction            | `FilenamePattern`, `MetadataConfig` |
| Sidecar      | Multipart field collection + base filename matching | `collectSidecarFiles`, `processRecord` |
| Embedded JSON| JSON parsing from `metadata` field            | `simulatedUpload`, `Media`, `parseMetaFile` |

The `config::MetadataConfig` allows control over which sources to use (`filename`, `sidecar`, `both`, or `auto`) [1], and the `help::MetadataDoc` confirms these four distinct modes [4].
```

---

## Q59. What transform types are available for metadata fields in the recording mover?

```

Based on the retrieved knowledge, there is **no explicit listing of available transform types** for metadata fields.

The only related information comes from **Fact 10**, which states:

- The `metadata::ApplyTransforms` function [returns an error for invalid transform types](internal/metadata/transform_test.go), implying that only certain predefined transform types are valid — but it does **not specify what those types are**.

Additionally:

- **Fact 4** and **Fact 8** mention that the `MetadataConfig` type supports `field_mappings`, `defaults`, `required`, and date/time layout fields — but these describe configuration options, not transform types.
- **Fact 18** notes that the `metadata::Service` uses configured transforms, again without enumerating them.

In summary, while the system supports transforms (and validates them), the specific set of available transform types (e.g., "trim", "lowercase", "regex", "date_format", etc.) is **not documented in the retrieved facts**.
```

---

## Q60. How does the Worker Manager reconcile company configurations every 10 minutes?

```

The retrieved knowledge does **not** contain any information about a "Worker Manager" reconciling company configurations every 10 minutes.

While the context describes several configuration management systems (e.g., `config::Manager` polling for AppConfig updates [Fact 6, 8, 12]), worker pools [Fact 5, 16], and runtime reconfiguration [Fact 3], **none mention**:

- A component called "Worker Manager"
- A reconciliation mechanism based on company configurations
- A 10-minute reconciliation interval

It is possible this functionality exists in code not captured by the retrieved facts, but based solely on the provided context, there is no evidence for how (or whether) such reconciliation occurs.
```

---

## Q61. What SQS message formats does the recording mover support?

```

The recording mover supports two SQS message formats:

1. **Native format** — Direct JSON messages that are parsed into `Message` structs, supporting `.wav` and `.mp3` audio file formats [Fact 15].

2. **S3 event format** — SQS messages containing AWS S3 event notifications, structured as `S3EventMessage` top-level JSON containers with nested `S3EventRecord`, `S3EventDetail`, and `S3ObjectDetail` fields. This format enables precise tracking of source object metadata for download [Facts 13, 14].

These formats are handled by the `message::Decode` function, which accommodates both native and S3 event message structures [Fact 15].
```

---

## Q62. How does the streaming multipart upload work via io.Pipe in the recording mover?

```

The streaming multipart upload via `io.Pipe` is implemented in the `uploader::HTTPClient` type, which is a concrete implementation of the `uploader::Client` interface [entity_name: uploader::HTTPClient, uploader::Client].

Specifically, the upload mechanism uses `io.Pipe` to stream audio data without loading the entire file into memory [fact: 1]. This is part of how it constructs multipart form uploads that include `api_key`, metadata (as a JSON string), optional extra fields, and the audio payload—either as a binary file part or as a base64-encoded string [fact: 10].

The use of `io.Pipe` enables asynchronous streaming: one goroutine writes the multipart form data into the pipe (producer), while another reads from it to send over the HTTP connection (consumer). This avoids buffering the entire request body in memory, which is crucial for handling large audio files efficiently.

While the exact `io.Pipe` integration is not detailed in the provided facts, its purpose is explicitly stated: to **avoid loading the entire file into memory** during upload [fact: 1]. This pattern aligns with idiomatic Go practices for handling streaming HTTP requests and is likely implemented inside the `Upload` method of `HTTPClient`.

No further internal implementation details (e.g., pipe initialization or goroutine setup) are present in the provided context.
```

---

## Q63. What are the normalized metadata fields and how is field resolution ordered in the recording mover?

```

The normalized metadata fields and field resolution order in the recording mover are driven by configuration-based parsing and transformation logic, with specific sources and sequencing defined across the codebase.

### Normalized Metadata Fields

The `metadata::Service` normalizes raw metadata from multiple sources (filename, sidecar files, or a combination) into a structured `Normalized` type using config-driven mappings [metadata::Service]. The `help::ConfigurationDoc` entity specifies that metadata configuration supports the following fields: `source`, `filename_pattern`, `field_mappings`, `defaults`, `required`, `date_layouts`, `time_layouts`, and `timezone` [help::ConfigurationDoc]. This configuration directly informs how raw fields are mapped to normalized output.

The `simulate::recordResult` type aggregates normalized metadata (as a map), raw metadata, sidecar details, fixed fields, and errors for a processed file [simulate::recordResult]. Its structure indicates that normalized metadata is a first-class output alongside raw input and processing artifacts.

The `processing::Manifest` type includes a generic `metadata` field, which stores normalized metadata as part of a persisted intake job record [processing::Manifest]. Similarly, `metadata::Normalized.ToMap` converts the normalized struct into a string map, used in JSON payload generation and storage [metadata::Normalized.ToMap].

Normalized fields are not enumerated explicitly in the retrieved facts, but their structure and transformations are configurable via `field_mappings` and defaults in the configuration. The `metadata::Service.Normalize` function returns both the normalized struct and a raw metadata map for debugging [metadata::Service.Normalize], implying that normalization applies transforms (regex, split, unix-to-RFC3339) and defaults defined in config [metadata::Service].

### Field Resolution Order

The field resolution order is implied by the processing flow:

1. **Initial merge**: The `processor::Processor` first merges additional fields (e.g., `CorrelationID` and source details) into the normalized metadata map [processor::Processor].
2. **Raw field extraction**: `metadata::Service.Normalize` extracts raw metadata from filename (via `filename_pattern`) and sidecar files, applying transforms and defaults [metadata::Service.Normalize].
3. **Post-initial merge**: The `processor::Processor.Process` function passes extra raw metadata fields into the normalized map after the initial merge [processor::Processor.Process], indicating a two-stage normalization and enrichment process.

The `config::PostProcessingConfig` uses `RecordingDate` as the default metadata field to build date paths, suggesting that `RecordingDate` is a canonical field expected in the normalized output [config::PostProcessingConfig].

In summary, normalized metadata fields are config-defined via mappings, defaults, and transforms, and field resolution follows a staged process: initial merge of known fields, extraction from filename/sidecar, application of transforms and defaults, followed by enrichment with additional raw fields.
```

---

## Q64. How does post-processing work in the recording mover?

```

Based on the retrieved facts, there is no explicit mention of a **"recording mover"** or its post-processing logic. However, we can infer related behavior from adjacent components:

- The `processor::Processor` type ([Fact 5]) orchestrates fetching, normalizing, and uploading recordings, and after upload, invokes either `moveFilesOnSuccess` or `moveFilesOnError` based on outcome. This suggests that file movement (including post-upload cleanup or archival) is handled conditionally by the processor, but the *details* of *how* post-processing works are not specified in the facts.

- The `simulate::handler` type ([Fact 1]) processes recording-like data via `handle()`, involving parsing media and sidecar files and producing per-record metadata — but this is part of a *simulation* module, not necessarily production mover logic.

- The `Media::handlePostback` function ([Fact 20]) processes postback JSON to update the media object’s document and transcript fields — indicating state updates after processing, but again not necessarily file movement.

**Conclusion:** The facts describe that file movement decisions (e.g., success/error paths) occur *after* upload in `processor::Processor`, but no explicit documentation or details of the *post-processing steps* performed by a "recording mover" exist in the retrieved context. To fully answer the question, we would need additional facts about the recording mover itself or the `moveFilesOnSuccess`/`moveFilesOnError` implementations.
```

---

## Q65. What are the three test tiers for the recording mover and what does each cover?

```

The provided knowledge base does not contain information about a "recording mover" or its test tiers. There is no mention of this service, component, or testing structure in any of the retrieved facts.

If you have additional context about the "recording mover" or where this testing information might be located, I’d be happy to help refine the search.
```

---

## Q66. How does the mover handle per-region S3 client caching?

```

The mover handles per-region S3 client caching through the `storage::S3Fetcher` type, which is defined in `internal/storage/storage.go` [1]. This entity explicitly caches S3 clients per region in a thread-safe map protected by a mutex [1]. The `S3Fetcher` implements fetching and moving files to/from AWS S3 using these cached, region-specific clients [1].

Additionally, the `storage::Factory` maintains separate maps for fetchers and movers keyed by storage type string, which includes support for S3-based operations [13]. This design supports efficient reuse of S3 clients across requests within the same region.

The caching behavior is further reinforced by the `config::TenantConfig`, which holds reusable AWS client instances—including `S3Client`—across requests, reducing the overhead of client initialization [11].
```

---

## Q67. How do services discover each other in the AgentAssist platform - is there a service registry or hardcoded URLs?

```

Based on the retrieved knowledge, there is **no evidence of a formal service registry or centralized URL discovery mechanism** in the AgentAssist platform. The platform appears to rely on **hardcoded paths, direct instantiation, and implicit dependencies** rather than dynamic service discovery.

Here’s what the facts indicate:

- Services are typically instantiated directly where needed. For example, `objAgents` is instantiated in `docroot/ajax/filters.php` for agent search operations [Fact 7], and `AgentGroupsController` is instantiated per request in `docroot/api/index.php` for the `/agent-groups/all` endpoint [Fact 5]. This suggests direct coupling rather than lookup via a registry.

- Endpoints and resources are referenced by relative file paths (e.g., `docroot/fetch_autocomplete.php` [Fact 11], `docroot/report_agent_talk.php` [Fact 10], `owr/scripts/create_agents_from_csv.php` [Fact 1])—indicating a filesystem-based routing system, not a service registry.

- Cross-company queries are handled via permission checks and fallbacks (e.g., `agents_cross_company` query type in `fetch_autocomplete.php` [Fact 11]), not via remote service invocation or URLs.

- The `Agent::searchAgentsCrossCompany()` method [Fact 18] and `objAgents` service [Fact 7] operate within the same application codebase, suggesting services do not communicate via external HTTP endpoints or service-to-service calls.

- Email delivery uses class-based SMTP configuration (`Mailer` class) [Fact 2], but no indication of dynamic endpoint resolution—only company-specific templates and SMTP settings.

- The `serve` endpoint for `atlaskb` [Fact 4] exposes knowledge graph tools via an MCP server, but this is for *external* agentic systems, not internal platform service discovery.

In conclusion: **There is no service registry or dynamic URL resolution mechanism observed. Services are discoverable only through direct instantiation, shared class libraries, and filesystem paths—typical of tightly coupled monolithic applications.**
```

---

## Q68. What is the AMQP queue topology across the AgentAssist platform?

```

Based on the retrieved knowledge, the AMQP queue topology across the AgentAssist platform is not fully documented as a single, unified architecture—no single source explicitly defines the complete topology (e.g., queue names, routing keys, exchange types, or inter-service flow). However, several facts allow us to reconstruct key aspects of the messaging topology:

### AMQP Brokers Used
- **RabbitMQ** is the primary AMQP broker across multiple services:
  - `vector-transcribe-llm-worker` consumes AMQP messages “from RabbitMQ” [Fact 1]
  - `vector-transcribe-qa` integrates with **AMQP (RabbitMQ)** as the “primary message bus” [Fact 6]
  - `vector-transcribe-intake` uses **RabbitMQ** as its async messaging backbone, with an `intake_queue` referenced via `streadway/amqp` publisher [Fact 13]
  - `vector-transcribe-redaction` integrates with **LavinMQ** (an AMQP 0-9-1-compatible broker) for job ingestion, retries, and dead-letter queue visibility [Fact 2]

### Key Queues Identified
- **intake_queue** — used by `vector-transcribe-intake` to publish messages containing `audioKey`/`metaKey` pairs for downstream processing [Fact 13, Fact 10]

No explicit queue names beyond `intake_queue` are named in the retrieved facts. However, logical queues can be inferred:
- `complete` queue — referenced in `vector-transcribe-qa` as the queue from which workers consume messages for postback and database updates [Fact 6, Fact 15]
- `job` or `transcription` queues — implicitly used by `vector-transcribe-redaction`, `vector-transcribe-postback`, and `vector-transcribe-llm-worker`, though not named

### Message Flow Pattern (Inferred Topology)
The messaging topology appears to follow a **decoupled, event-driven pipeline**:
1. **Intake** → `vector-transcribe-intake` accepts requests, stores audio/metadata in S3, and publishes to `intake_queue` in RabbitMQ [Fact 13, Fact 14]
2. **Upstream processing** — services like `vector-transcribe-qa`, `vector-transcribe-llm-worker`, and `vector-transcribe-redaction` consume from queues (e.g., `complete`, `intake`, or unnamed job queues), process messages (e.g., transcription, LLM QA, redaction), and possibly publish to downstream queues (not explicitly stated)
3. **Postback** — `vector-transcribe-postback` consumes jobs from an AMQP queue (presumably a dedicated queue), decrypts files, and sends HTTP callbacks [Fact 5, Fact 11]

### Architectural Patterns
- **Worker pools with bounded backpressure** — `asyncapi::API.StartConsumers` in `vector-transcribe-postback` uses a single AMQP consumer goroutine feeding a buffered channel to N worker goroutines, controlling parallelism and queue depth [Fact 14]
- **Layered architecture** — services like `vector-transcribe-qa` decouple AMQP coordination (`asyncapi::API`), configuration (`config.Manager`), database access (DocumentDB/MySQL), and encryption logic [Fact 15]
- **Hot-reloading configuration** — all key services use AWS AppConfig for dynamic configuration of AMQP queue names, connection parameters, and forwarding URLs [e.g., Fact 10, Fact 13]

### Limitations
- There is no evidence of **exchanges** being defined or used (direct, fanout, topic).
- No mention of **dead-letter queues (DLQs)** configuration beyond visibility in `vector-transcribe-redaction` [Fact 2].
- Queue naming beyond `intake_queue` is implicit.

**Conclusion**: The AMQP topology centers around RabbitMQ (with LavinMQ in one service), uses a few named queues (`intake_queue`, and implicitly `complete`), and follows a producer/consumer pattern with worker pools and hot-reloadable configuration. A full topology diagram is not reconstructable from the provided facts.
```

---

## Q69. How does the platform handle end-to-end encryption of sensitive audio data?

```

The platform implements a layered, service-oriented encryption architecture where sensitive audio data is encrypted at ingestion, stored encrypted in AWS S3, processed through encrypted channels, and decrypted only at authorized endpoints with appropriate security controls. Here’s how end-to-end encryption is handled:

### 1. **Encryption at Ingestion**
- The `vector-transcribe-intake` service (Go-based) receives audio and metadata, then uses an `encrypt::Client` to encrypt the audio before uploading it to S3 [Fact 3].
- Similarly, in `internal/asyncapi/upload.go`, the `API.QueueWavFile` function calls `encryptService.EncryptFileS3` to encrypt and upload audio files to S3 before publishing messages [Fact 7].

### 2. **Secure Storage**
- Encrypted audio files and associated metadata are stored in AWS S3 across multiple services: `vector-transcribe-intake`, `vector-transcribe-redaction`, and `vector-transcribe-postback` all use S3 as a storage layer for encrypted artifacts [Facts 2, 3, 4].

### 3. **Decryption at Consumption**
- The `amqpconsumer::Consumer` (in Go) consumes AMQP messages containing S3 keys (`audioKey`, `metaKey`), downloads the encrypted files, and decrypts them using a configured encryption client before forwarding to the transcription service as multipart/form-data [Fact 17].
- The `vector-transcribe-postback` service decrypts files retrieved from S3 (transcripts and optional audio) using a circuit-breaker-protected encryption service [Fact 4].
- The `agent-docroot::playback` endpoint retrieves encrypted audio from S3, decrypts it using `Encrypt::decrypt` (via static method `decryptFileS3()` in PHP scripts), and streams decrypted WAV content with proper HTTP range headers [Facts 1, 5, 6].

### 4. **Encryption Abstraction and Transport**
- A shared `Envelope` type (defined in `internal/handlers/encrypt_shared.go` and `internal/vectorencryption/vectorencryption.go`) serves as a standardized container for encrypted payloads—comprising base64-encoded ciphertext and metadata (e.g., encryption parameters)—used across `/encrypt` and `/decrypt` endpoints [Facts 12, 16, 19].
- Services enforce strict contracts: in `vector-transcribe-redaction`, the `Database` module must call `EncryptionClient` methods for decryption/encryption, expecting either success or `EncryptionError` [Fact 11].

### 5. **Operational Security**
- Temp files containing decrypted audio (e.g., during reprocessing via `owr/scripts/reprocess_all_media.php`) are generated using `tempnam` (secure temp file creation) and cleaned up after use [Facts 8, 15].
- The platform enforces SSRF protection, DNS-time IP validation, and encrypted messaging (e.g., encrypted AMQP) to prevent unauthorized access during transit and processing [Facts 4, 13].

### 6. **Recent Enhancements**
- Encryption was formalized in `vector-transcribe-asr-api` with an `encrypt_service.py` module on 2025-08-18, integrating base64 audio decoding to support encrypted payloads [Fact 10].
- The `vector-transcribe-llm-worker` service extends encryption to LLM inputs/outputs via `EncryptionClient`, ensuring prompt/response obfuscation [Fact 9].

In summary, encryption is consistently applied via a dedicated encryption service and client abstractions, with strict separation between encrypted storage/transit and authorized decryption for legitimate processing endpoints. Hardcoded formats (e.g., WAV in playback) and explicit error handling reflect a mature, auditable security posture [Fact 1, Fact 11].
```

---

## Q70. What monitoring and observability patterns are consistent across the Go services?

```

Based on the retrieved facts, the Go services exhibit several consistent monitoring and observability patterns:

### 1. **Structured Logging with Context Propagation**
All Go services utilize structured logging integrated with Go’s `context` to propagate request-scoped metadata (e.g., request IDs, loggers) across layers. Specifically:
- `vector-transcribe-audio-api` uses `logging::FromContext`/`ContextWithLogger` and `requestid::Middleware` to propagate observability context [Fact 1].
- This aligns with the broader `taskflow.logging` package added on 2024-07-01, indicating a centralized awareness of observability needs [Fact 8].

### 2. **Health and Readiness Endpoints**
Multiple services expose Kubernetes-compatible health and readiness endpoints as part of a dedicated HTTP health server:
- `vector-transcribe-llm-worker` implements a `HealthServer` with `/health`, `/health/ready`, `/health/live`, and Prometheus-compatible metrics endpoints [Fact 5].
- `vector-transcribe-postback` includes similar endpoints (`/health`, `/health/ready`, `/health/live`, `/metrics`) [Fact 6], demonstrating alignment in operational interface design.

### 3. **Prometheus Metrics Integration**
Prometheus metrics are consistently exposed:
- `vector-transcribe-llm-worker` batches CloudWatch-compatible metrics via an emitter [Fact 4].
- `vector-transcribe-postback` and `vector-transcribe-qa` expose Prometheus metrics with service-specific prefixes (`postback_*` and throughput/errors/in-flight metrics respectively) [Fact 6, Fact 20].
- `metrics::Metrics` in one service implements throughput logging (every 60 seconds) and Prometheus exposure [Fact 16].

### 4. **Configuration Change Observability**
Dynamic configuration management includes observability mechanisms:
- `vector-transcribe-audio-api`’s `config::WatchForChanges` logs changes and triggers callbacks only when changes occur, supporting traceability of configuration drift [Fact 3].
- Similar hot-reload mechanisms with logging exist in `vector-transcribe-redaction` and `vector-transcribe-intake`, though observability specifics are implied rather than explicitly detailed [Facts 18, 19].

### 5. **Connection and Reconnection Logging**
Connection lifecycle events are logged for observability and debugging:
- Functions like `owr.scripts::getAmqpChannel` log reconnection events to the global logger [Fact 7].
- `process_automated_scorecards.php` logs scorecard aggregation results and deduplication skips [Fact 17], suggesting similar logging practices across AMQP-based services.

### 6. **Circuit Breaker Pattern**
Circuit breakers are used both for resilience and observability:
- `vector-transcribe-postback` integrates circuit breakers around encryption service calls [Fact 6], likely exposing failure metrics or state for monitoring.

---

#### **Notable Gap**
One service explicitly states it lacks advanced observability beyond structured logs:
- `vector-transcribe-audio-api` has *“limited observability beyond structured logs (no metrics, traces, or distributed tracing integration)”* [Fact 9], indicating inconsistency in instrumentation depth.

Overall, while core observability practices—structured logging, health checks, and Prometheus metrics—are well-standardized, the use of distributed tracing and more advanced observability features remains fragmented.
```

---

## Q71. How does AWS AppConfig integrate across multiple services in the platform?

```

AWS AppConfig is integrated across multiple services in the platform as a **centralized, dynamic configuration source** that enables runtime configuration updates without service redeployment. Across the vector-* family of services, AppConfig follows a consistent architectural pattern involving background polling, configuration hot-reload, and modular configuration management.

### Core Integration Pattern

1. **Configuration Loading & Polling**:
   - Services use `config.NewManager` (Go) or `AppConfigPoller` (Python) to initialize configuration via AWS AppConfig [fact 8, fact 17].
   - The `config::Manager` component fetches and parses INI-style configuration payloads from AppConfig, with background polling loops (default 30s interval) [fact 13, fact 18].
   - Configuration precedence typically follows: defaults → local files (INI/YAML) → environment variables → AWS AppConfig → global overrides [fact 11].

2. **Hot-Reload Support**:
   - Services subscribe to configuration updates via channels (e.g., `Updates()` in Go) to react to changes without restart [fact 8, fact 17].
   - The `AppConfigPoller` in `vector-transcribe-llm-worker` diffs configuration payloads and invokes targeted callbacks based on change categories (infrastructure vs. tuning keys), enabling fine-grained hot-reconfiguration [fact 12].

3. **Fallback & Testing Support**:
   - Services support fallback to local INI files if AppConfig fails (e.g., `vector-transcribe-intake`, `vector-transcribe-recording-mover-v2`) [fact 10, fact 11].
   - Mock implementations like `config::fakeAppConfig` provide testable AppConfig behavior without AWS dependencies [fact 14].

### Service-Specific Use Cases

| Service | AppConfig Use Case | Dimension |
|--------|---------------------|-----------|
| `vector-encrypt` | Dynamic, per-tenant settings with hot-reload | behavior [fact 1] |
| `vector-transcribe-intake` | ConfigStore with polling-based hot reload, fallback to local INI | behavior, pattern [fact 10, fact 15] |
| `vector-transcribe-recording-mover-v2` | Remote configuration source; superseded static `config.ini` by 2026-01-16 | behavior, pattern [fact 2, fact 16] |
| `vector-transcribe-postback` | Runtime config for queue names, encryption service endpoints, polling behavior | behavior [fact 3] |
| `vector-transcribe-redaction` | Dynamic runtime/infrastructure config via `aioboto3.appconfigdata` | behavior [fact 4] |
| `vector-transcribe-audio-api` | Dynamic polling, caching to disk, INI parsing | behavior [fact 5] |
| `vector-transcribe-llm-worker` | Dynamic reconfiguration via `AppConfigPoller` with diff-based callbacks | behavior, pattern [fact 6, fact 12] |
| `vector-transcribe-qa` | Sources AMQP, DB, HTTP, timeout config with hot-reload via polling | behavior, pattern [fact 7, fact 18] |

### Supporting Infrastructure

- **`config::Manager`**: Central configuration manager that polls AppConfig, parses INI, exposes live config via `Manager.Current()` [fact 13].
- **`config::AppConfigSettings`**: Encapsulates connection parameters (e.g., app ID, config ID, environment) for AWS AppConfig [fact 20].
- **Testing Infrastructure**: Mock AppConfig client (`fakeAppConfig`) enables deterministic testing of configuration loading and updates [fact 14].

In summary, AWS AppConfig functions as a **configuration-as-a-service** hub across the platform, unifying runtime flexibility, safe hot-reconfiguration, and testability into a consistent, production-grade model.
```

---

## Q72. What is the deployment strategy - containers, systemd, or both?

```

The deployment strategy varies across services, with **no uniform approach**:

- **`vector-transcribe-audio-api`** uses **systemd** (specifically transitioned from systemd template services to a single service on 2026-02-10) [vector-transcribe-audio-api].

- **`vector-transcribe-postback`** also uses **systemd** (with systemd service templating as part of its modernized infrastructure) [vector-transcribe-postback].

- **`vector-transcribe-recording-mover-v2`** and **`vector-transcribe-llm-worker`** both use **Buddy CI/CD pipelines** for deployment, but neither explicitly mentions containers or systemd in their summaries [vector-transcribe-recording-mover-v2, vector-transcribe-llm-worker].

- **`atlaskb-typescript-test-repo`** has no deployment mechanism described at all [atlaskb-typescript-test-repo].

- **`atlaskb::ingestion-layer`** uses a **single-node architecture** where API server, workers, and PostgreSQL coexist on the same machine — consistent with systemd-based deployment but not explicitly stated [atlaskb::ingestion-layer].

No evidence of containerized deployments (e.g., Docker/Kubernetes) is present in the provided facts.

Thus, **systemd is the explicit deployment mechanism for the vector-transcribe services**, but containerization or other deployment strategies may be used elsewhere; current context doesn't confirm containers anywhere.
```

---

## Q73. How does the platform handle multi-tenant multi-company isolation?

```

The platform implements multi-tenant, multi-company isolation through several coordinated mechanisms:

1. **Database-level isolation via company UUID**: The `AlertCase` model enforces tenant isolation by always including `company_uuid` in all database queries and index lookups, ensuring data is scoped to the appropriate company [class.alert_case::AlertCase].

2. **Per-tenant configuration with AWS AppConfig**: Services like `vector-encrypt` and `vector-transcribe-recording-mover-v2` use AWS AppConfig for dynamic, per-tenant configuration loading and hot-reloading of settings, enabling runtime updates without redeployment [vector-encrypt, vector-transcribe-recording-mover-v2]. The `TenantConfig` struct holds tenant-specific metadata (Name, APIKey, Region, AllowedKeyIDs, DefaultKeyID, Endpoint) along with dedicated AWS clients [config::TenantConfig].

3. **INI-based configuration parsing with per-tenant sections**: Configuration parsing (`ParseFromBytes`, `parseFromBytes`) processes INI files with sections prefixed by `api ` to build isolated per-tenant configurations, supporting multi-tenancy through structured key separation [config::ParseFromBytes, config::parseFromBytes].

4. **Session-aware context management**: The session service stores both current and original company objects separately, supporting company switching functionality while maintaining contextual awareness [docroot.ajax::Session].

5. **Permission-based cross-company access control**: Cross-company operations are restricted and validated explicitly. For example, agent assignment across companies requires explicit permissions and super admin or linked-company access rights [docroot::agent_user.php]. Similarly, `Permission` records are not company-unique, allowing permissions to be shared across companies when appropriate, but enforcement is still contextual [class.permission::Permission].

6. **Architectural support for separation of concerns**: Services like `vector-transcribe-intake` and the `knowledge_model` follow modular/hexagonal architectures, cleanly separating configuration, database access, and business logic into distinct subsystems that can enforce isolation boundaries [vector-transcribe-intake, atlaskb::knowledge_model::entity::function].

7. **Platform naming policy flexibility**: The platform supports company-specific or shared naming policies, indicating deliberate design for both isolation and potential sharing of resources [class.company::Company].

Together, these mechanisms ensure strong multi-tenant and multi-company isolation while supporting flexibility for cross-company operations where explicitly permitted.
```

---

## Q74. What happens to a recording if the redaction service is down?

```

Based on the provided facts, there is no explicit description of what happens to a recording if the redaction service is down. The facts describe:

- Redaction behavior (e.g., `src::AMQPData` stores redaction output paths [Fact 1], `src::redact_audio` creates redacted audio files [Fact 4], `docroot::Redaction` manages redaction records [Fact 19]),
- Transcript and audio processing workflows (e.g., reprocessing scripts [Facts 2, 9, 16], decryption and callback workflows [Facts 10, 12]),
- Constraints around transcript completeness (e.g., `docroot::Transcript` and `agent-docroot::Transcript` only load transcripts for media with 'complete' status [Facts 5, 8]).

However, none of the facts explicitly address:
- Dependency on the redaction service’s availability,
- Failure handling if redaction fails (e.g., retries, fallbacks, or error propagation),
- Whether recordings can be processed or delivered without redaction,
- What status or state a recording would have if redaction fails.

In particular, while `owr.scripts::bulk_transcript_reprocess_from_json` checks for `audio_path_original` before attempting download [Fact 14], and `bulk_transcript_reprocess_from_json` respects the `skip_redaction` field [Fact 2], there is no information about failure behavior when redaction itself fails or is unavailable.

**Conclusion**: The provided context does not contain sufficient information to determine what happens to a recording if the redaction service is down.
```

---

## Q75. What is the error handling philosophy across the platform - retry vs DLQ?

```

The error handling philosophy across the platform combines **retry with exponential backoff** as the primary mechanism for transient failures, but increasingly favors **DLQ (Dead Letter Queue) requeuing** over permanent failure for durable recovery—especially in newer services.

### 1. **Retry with Exponential Backoff is the Default for Transient Failures**
Multiple services consistently use exponential backoff with jitter for resilience against transient issues:
- `vector-transcribe-llm-worker`: Uses exponential backoff with jitter (capped at 900s) for session/connection errors, while logging generic errors individually [error_handling] [vector-transcribe-llm-worker].
- `vector-transcribe-asr-api`: Employs exponential backoff with base delay of 2.0s, with `_PUBLISH_RETRIES = 3` hardcoded for AMQP publishing and connection establishment [error_handling] [vector-transcribe-asr-api].
- `vector-transcribe-intake`: Implements a 5-second delay retry loop for publishing/processing errors and uses `safeInvoke` to recover panics in subscriber callbacks [error_handling] [vector-transcribe-intake].
- `llm::OpenAIClient`: Retries up to 3 times with exponential backoff [llm::OpenAIClient.Complete].
- `src::RequestLogger`: Enforces up to 3 attempts with exponential backoff (2^attempt seconds) and 50% jitter [src::RequestLogger].
- `class.search::Search`: Uses bounded retry with exponential backoff and jitter for transient errors like GOAWAY, timeouts, 5xx, and curl errors [class.search::Search].

Some retry policies are more specific:
- `src::EncryptionClient`: Only retries on 5xx or request errors—not on 4xx—to avoid retrying client errors [src::EncryptionClient].
- `owr.scripts::main`: Uses up to 3 attempts with fixed 5-second delays [owr.scripts::main].
- `bus::Bus`: Uses linear backoff (`retryDelay * attempt`) rather than exponential [bus::Bus].

### 2. **DLQ-Based Requeuing is Preferred for Guaranteed Delivery**
Several services now treat failures as retriable rather than terminal, leveraging DLQ mechanisms for durability:
- `vector-transcribe-redaction`: As of 2026-02-14, *all* message failure paths now use LavinMQ DLQ (requeue=True), including previously non-retriable errors like `DocumentDBNotFoundError` and `ValueError` [vector-transcribe-redaction].
- `vector-transcribe-async-api`: Implements durable error recovery via disk-persisted messages and `asyncapi::API.loadErrorMessages`, which scans error directories and re-publishes failed jobs to LavinMQ [vector-transcribe-async-api].
- `asyncapi::API.loadErrorMessages`: Re-publishes failed messages with fixed priority (5) and deletes files only on success—ensuring eventual consistency [asyncapi::API.loadErrorMessages].
- `atlaskb-typescript-test-repo`: Uses a queue-based retry architecture (`InMemoryQueue` + `ExponentialBackoff`) and decouples retries via `router::EventRouter.processRetries` [atlaskb-typescript-test-repo].
- `help::SupportDoc`: Relies on SQS redelivery based on visibility timeout for retries [help::SupportDoc].

### 3. **Exceptions and Failures are Categorized**
Services distinguish between retriable and non-retriable errors:
- `vector-transcribe-qa`: Nacks messages with requeue decisions based on failure type—decryption failures are retried, unmarshal failures are dropped [error_handling] [vector-transcribe-qa].
- `vector-transcribe-llm-worker`: Uses custom exceptions (e.g., `PromptTooLargeError`, `EncryptionError`) to signal domain-specific, often non-retriable, failures [error_handling] [vector-transcribe-llm-worker].

### 4. **Graceful Degradation and Observability**
Retry logic is embedded within layered, observability-first architectures:
- Structured logging, metrics, and Prometheus integration accompany retry behavior across services.
- `vector-transcribe-asr-api` and `vector-transcribe-llm-worker` both implement CUDA OOM handling and graceful shutdown, indicating resilience is designed into core workflows.

### Summary
The platform’s philosophy is evolving toward **retry-first with DLQ as a durable fallback**, moving away from NACK-without-requeue toward requeuing failures (as seen in `vector-transcribe-redaction`). Retry is standardized around exponential backoff (with jitter where noted), with limits and exception categorization to avoid infinite loops or wasted retries on client errors. DLQ usage reflects a shift toward *durable, eventual-consistency patterns* over immediate failure.
```

---

## Q76. How does the redaction service _pause_for_recovery prevent multiple concurrent recovery loops?

```

The redaction service prevents multiple concurrent recovery loops by using a `_reconnecting` flag, as evidenced in the test suite for hot-reloading [tests::TestReconfigure] [10]. This flag serves as a guard to prevent concurrent reconfiguration or recovery attempts, ensuring only one recovery loop runs at a time.

While `pause_for_recovery` itself is described in the Worker entity as part of the exponential backoff probe loop pattern for reconnection [src::Worker] [1], the specific concurrency protection mechanism (the `_reconnecting` flag) is explicitly documented in the context of `Worker.reconfigure()` and validated in tests.

This pattern—using a boolean flag to serialize recovery attempts—is consistent with best practices for avoiding race conditions during service recovery, especially when reinitializing AMQP connections, process pools, and internal state [src::Worker], [tests::TestReconfigure] [10].
```

---

## Q77. What is the AMQP QOS prefetch strategy and how does it relate to concurrency?

```

The AMQP QoS (Quality of Service) prefetch strategy is used to control message flow and fairness in message processing, directly impacting concurrency. Based on the retrieved facts:

### Prefetch Strategy
- **QoS prefetch** is used to control message flow and fairness in AMQP consumers. For example, `asyncapi::API.consumeCompleteQueue` explicitly uses QoS prefetch for this purpose [Fact 1].
- In several services, a **prefetch count of 1** is configured to ensure **fair dispatch**, meaning messages are distributed evenly among consumers instead of being hoarded by fast consumers [Fact 3, Fact 7].

### Relationship to Concurrency
- In `src::Worker`, the `prefetch_count` is derived from `config.concurrency`, meaning the number of concurrent messages a consumer will handle is directly tied to the configured concurrency level [Fact 5, Fact 12].
- `src::Worker` also processes messages concurrently **up to** `prefetch_count`, indicating that the QoS setting effectively bounds concurrency per consumer channel [Fact 12, Fact 13].
- In `vector-transcribe-qa`, the service enforces **channel recycling and idle timeout detection**, and the prefetch setting plays into this lifecycle management to maintain healthy, balanced concurrency over time [Fact 11].
- In `vector-transcribe-postback`, a different concurrency model is used: a **single AMQP consumer goroutine** feeds deliveries into a **worker pool** of `N` goroutines via a buffered channel, enabling controlled parallelism with bounded queue depth. Here, prefetch helps decouple AMQP-level fairness from internal Go-level parallelism, though the actual concurrency is managed at the worker pool level [Fact 20].

### Summary
The QoS prefetch strategy serves two main purposes:
1. **Fairness**: A prefetch count of 1 ensures balanced load distribution across consumers.
2. **Concurrency Control**: Prefetch count often maps directly to concurrency limits (e.g., `config.concurrency` in `Worker`), and in some architectures, it’s complemented by worker pools or connection/channel lifecycle management for robust, scalable concurrency.

The choice between using prefetch for concurrency control (direct model) vs. prefetch + worker pool (indirect model) depends on service design goals such as thread safety, resource isolation, and resilience [e.g., vector-transcribe-asr-api per-thread publishers, vector-transcribe-qa channel recycling] [Fact 2, Fact 8, Fact 11].
```

---

## Q78. How does the mover handle S3 event notification messages vs native v2 messages differently?

```

The mover handles S3 event notification messages and native v2 messages differently primarily in how they are parsed and validated upon ingestion, though the downstream processing (e.g., moving files on error) does not distinguish based on source format.

Specifically:

- **Parsing Strategy**: The `message::Decode` function prioritizes S3 event format over native format. It first attempts to parse the incoming JSON as an `S3EventMessage`, then falls back to the native `Message` format [Fact 1, Fact 7]. This indicates the system expects S3 event messages (typically from SQS-S3 notifications) to be the primary input, but supports fallback for backward compatibility.

- **Validation Constraints**: For S3 event messages, validation enforces that the bucket name and object key must be non-empty. If either is missing or empty, `Decode` returns an error [Fact 9, Fact 13]. There is no mention of equivalent validation for native format—implying it may have looser or different constraints.

- **Metadata Extraction**: When parsing S3 event messages, the system extracts bucket and object key from `s3.bucket.name` and `s3.object.key`, respectively [Fact 11]. For native format, no such extraction is specified—suggesting the bucket and key may be provided directly as top-level fields.

- **Company Field Handling**: A critical difference is that the `company` field is *not* extracted from S3 event format messages—it remains empty after decoding [Fact 17]. This could affect routing or processing logic downstream, depending on how the mover relies on the `company` field.

- **No Explicit Malformed Record Handling**: The `vector-transcribe-recording-mover-v2` service has *no explicit handling* of malformed S3 event records beyond the decoding fallback strategy [Fact 3]. This contrasts with the AMQP-based `asyncapi::handleDelivery`, which explicitly records dead-lettered messages with specific requeue reasons (e.g., missing S3 fields) [Fact 19]. This suggests S3 event message errors in the mover may result in silent message loss or misrouting.

- **Downstream Behavior**: Once decoded, the mover treats both message types similarly during file operations. For example, `processor::moveFilesOnError` only acts on media of type `'s3'` and whether the storage backend has a mover capability—regardless of whether the message originated from S3 events or native format [Fact 5].

In summary, the mover’s primary differences lie in *decoding and validation*, not in core movement logic. S3 event messages require more structured metadata (bucket, key), are validated strictly, and lack company field support, while native format appears more flexible but is tried secondarily during decoding.
```

---

## Q79. What happens during a hot-reload of the AMQP URL in the redaction service - how are in-flight messages handled?

```

During a hot-reload of the AMQP URL in the redaction service (specifically in `src::Worker`), the following sequence occurs:

- **In-flight messages are drained** before applying the new configuration. This ensures that any messages currently being processed are completed or appropriately handled before the connection is torn down [src::Worker].

- The worker **closes existing connections**, applies the new configuration (including the updated AMQP URL), and **re-registers consumers** to begin consuming from the reconnected AMQP instance [src::Worker].

- This process is **configured with a timeout**, allowing the system to enforce bounds on how long draining can take [src::Worker].

The redaction service’s `Worker` type is designed to handle reconfiguration robustly, managing AMQP connections, process pools, and health during such transitions [src::Worker].
```

---

## Q80. How does the redaction service throughput stats logging work and what does it report?

```

The redaction service's throughput stats logging works through the `Worker` component in `src/consumer.py`, which records and reports throughput metrics every 60 seconds. According to Fact 2 and Fact 6, this async AMQP consumer records throughput stats (including completed, failed, requeued, and average duration) every 60 seconds and logs a summary with token usage.

The metrics reported in these throughput summaries are defined by the `THROUGHPUT_PATTERN` concept in `analyze.py` (Facts 3 and 11). Specifically, throughput log lines encode aggregate job metrics such as:

- Jobs processed
- Rate per minute  
- Completed/failed/requeued counts
- Average duration
- In-flight job count

Additionally, the `Worker` type logs and emits metrics for token usage, including prompt tokens, completion tokens, cached tokens, and elapsed times (Fact 15).

The logging implementation relies on a structured logging approach where `Worker` integrates with the overall logging infrastructure defined in `src/logger.py`. The `_sink_filter` function there filters logs based on a configurable `redaction_log_level` threshold (Facts 1, 8, 14), ensuring only logs above the configured level are emitted for the `vector_redaction_service`. The `update_redaction_log_level` function (Fact 1) allows dynamic adjustment of this threshold.

In summary, the redaction service performs periodic (60-second) logging of throughput stats that include job counts, success/failure/requeue metrics, average processing duration, in-flight jobs, and detailed token usage information—all filtered by a configurable log level.
```

