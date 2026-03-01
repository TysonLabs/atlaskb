#!/usr/bin/env bash
# create-typescript-test-repo.sh — Generates a TypeScript test repo for AtlasKB pipeline benchmarking.
# Creates a "WebhookRelay" Express project (~1800-2000 lines of TypeScript) designed to stress TS-specific patterns.
set -euo pipefail

REPO_DIR="/tmp/atlaskb-typescript-test-repo"

echo "Creating TypeScript test repo at $REPO_DIR..."
rm -rf "$REPO_DIR"
mkdir -p "$REPO_DIR"
cd "$REPO_DIR"
git init -q
git checkout -b main 2>/dev/null || true

# Helper: commit with a fixed date for reproducible git history
commit() {
    local msg="$1"
    local date="$2"
    GIT_AUTHOR_DATE="$date" GIT_COMMITTER_DATE="$date" \
        git add -A && git commit -q -m "$msg" --date="$date"
}

# ============================================================
# Commit 1: Project setup
# ============================================================
cat > package.json << 'EOF'
{
  "name": "webhook-relay",
  "version": "1.0.0",
  "description": "A configurable webhook relay and notification service",
  "main": "dist/index.js",
  "scripts": {
    "build": "tsc",
    "start": "node dist/index.js",
    "dev": "ts-node-dev --respawn src/index.ts",
    "test": "vitest run",
    "test:watch": "vitest",
    "lint": "eslint src/ tests/"
  },
  "dependencies": {
    "express": "^4.18.2",
    "uuid": "^9.0.0",
    "winston": "^3.11.0",
    "node-fetch": "^3.3.2"
  },
  "devDependencies": {
    "@types/express": "^4.17.21",
    "@types/node": "^20.11.0",
    "@types/uuid": "^9.0.7",
    "eslint": "^8.56.0",
    "@typescript-eslint/eslint-plugin": "^6.19.0",
    "@typescript-eslint/parser": "^6.19.0",
    "ts-node-dev": "^2.0.0",
    "typescript": "^5.3.3",
    "vitest": "^1.2.0"
  },
  "engines": {
    "node": ">=20.0.0"
  }
}
EOF

cat > tsconfig.json << 'EOF'
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "commonjs",
    "lib": ["ES2022"],
    "outDir": "./dist",
    "rootDir": "./src",
    "strict": true,
    "esModuleInterop": true,
    "skipLibCheck": true,
    "forceConsistentCasingInFileNames": true,
    "resolveJsonModule": true,
    "declaration": true,
    "declarationMap": true,
    "sourceMap": true,
    "experimentalDecorators": true,
    "emitDecoratorMetadata": true,
    "strictNullChecks": true,
    "noUncheckedIndexedAccess": true
  },
  "include": ["src/**/*"],
  "exclude": ["node_modules", "dist", "tests"]
}
EOF

cat > .eslintrc.json << 'EOF'
{
  "parser": "@typescript-eslint/parser",
  "plugins": ["@typescript-eslint"],
  "extends": [
    "eslint:recommended",
    "plugin:@typescript-eslint/recommended",
    "plugin:@typescript-eslint/recommended-requiring-type-checking"
  ],
  "parserOptions": {
    "project": "./tsconfig.json"
  },
  "rules": {
    "@typescript-eslint/explicit-function-return-type": "warn",
    "@typescript-eslint/no-unused-vars": ["error", { "argsIgnorePattern": "^_" }],
    "@typescript-eslint/no-floating-promises": "error"
  }
}
EOF

cat > README.md << 'EOF'
# WebhookRelay

A configurable webhook relay and notification service built with Express and TypeScript.

## Features

- Receive webhook events via HTTP POST
- Route events through configurable channels (Slack, Email, HTTP)
- Filter events by headers, payload content, or custom rules
- Retry failed deliveries with exponential backoff
- Event logging and audit trail
- Admin API for configuration management

## Architecture

WebhookRelay uses a pipeline architecture:

1. **Ingest** — Events arrive via HTTP POST to `/webhooks/:source`
2. **Filter** — Events pass through a configurable filter chain
3. **Route** — EventRouter fans out to matching channels
4. **Deliver** — Each channel delivers via its transport (HTTP, SMTP, Slack API)
5. **Retry** — Failed deliveries enter the retry queue with backoff

## Quick Start

```bash
npm install
npm run dev
```

## Configuration

Set environment variables or create a `.env` file:

- `PORT` — Server port (default: 3000)
- `LOG_LEVEL` — Logging level (default: info)
- `RETRY_MAX_ATTEMPTS` — Max retry attempts (default: 5)
- `SLACK_WEBHOOK_URL` — Slack incoming webhook URL
- `SMTP_HOST` — SMTP server host
- `SMTP_PORT` — SMTP server port
EOF

commit "Initial project setup" "2024-03-01T09:00:00"

# ============================================================
# Commit 2: Core types & interfaces
# ============================================================
mkdir -p src/types

cat > src/types/event.ts << 'EOF'
import { v4 as uuidv4 } from 'uuid';

/** Event priority levels */
export enum Priority {
  Low = 'low',
  Normal = 'normal',
  High = 'high',
  Critical = 'critical',
}

/** Lifecycle status of an event */
export enum EventStatus {
  Received = 'received',
  Filtered = 'filtered',
  Routing = 'routing',
  Delivered = 'delivered',
  Failed = 'failed',
  Retrying = 'retrying',
}

/** Channel transport types */
export enum ChannelType {
  Http = 'http',
  Slack = 'slack',
  Email = 'email',
}

/** Discriminated union for event kinds */
export type EventKind =
  | { kind: 'webhook'; source: string; headers: Record<string, string> }
  | { kind: 'system'; component: string }
  | { kind: 'scheduled'; cronExpression: string };

/** Type guard for webhook events */
export function isWebhookEvent(event: EventKind): event is EventKind & { kind: 'webhook' } {
  return event.kind === 'webhook';
}

/** Type guard for system events */
export function isSystemEvent(event: EventKind): event is EventKind & { kind: 'system' } {
  return event.kind === 'system';
}

/** Core event interface */
export interface Event {
  id: string;
  type: string;
  payload: Record<string, unknown>;
  metadata: EventMetadata;
  status: EventStatus;
  priority: Priority;
  eventKind: EventKind;
  createdAt: Date;
  updatedAt: Date;
}

/** Event metadata carried through the pipeline */
export interface EventMetadata {
  source: string;
  correlationId: string;
  retryCount: number;
  tags: string[];
  traceId?: string;
}

/** Summary projection — uses Pick utility type */
export type EventSummary = Pick<Event, 'id' | 'type' | 'status' | 'priority' | 'createdAt'>;

/** Factory function for creating events */
export function createEvent(
  type: string,
  payload: Record<string, unknown>,
  eventKind: EventKind,
  priority: Priority = Priority.Normal
): Event {
  const now = new Date();
  return {
    id: uuidv4(),
    type,
    payload,
    metadata: {
      source: eventKind.kind === 'webhook' ? eventKind.source : 'internal',
      correlationId: uuidv4(),
      retryCount: 0,
      tags: [],
    },
    status: EventStatus.Received,
    priority,
    eventKind,
    createdAt: now,
    updatedAt: now,
  };
}
EOF

cat > src/types/result.ts << 'EOF'
/**
 * Generic Result type for typed error handling.
 * Avoids throwing exceptions in business logic.
 */
export type Result<T, E = Error> =
  | { ok: true; value: T }
  | { ok: false; error: E };

/** Create a successful result */
export function Ok<T>(value: T): Result<T, never> {
  return { ok: true, value };
}

/** Create a failed result */
export function Err<E>(error: E): Result<never, E> {
  return { ok: false, error };
}

/** Unwrap a result, throwing if it's an error */
export function unwrap<T, E>(result: Result<T, E>): T {
  if (result.ok) {
    return result.value;
  }
  throw result.error instanceof Error ? result.error : new Error(String(result.error));
}

/** Map over a successful result */
export function mapResult<T, U, E>(result: Result<T, E>, fn: (value: T) => U): Result<U, E> {
  if (result.ok) {
    return Ok(fn(result.value));
  }
  return result;
}

/** Chain results together */
export function flatMap<T, U, E>(
  result: Result<T, E>,
  fn: (value: T) => Result<U, E>
): Result<U, E> {
  if (result.ok) {
    return fn(result.value);
  }
  return result;
}

/** Collect an array of results into a single result */
export function collectResults<T, E>(results: Result<T, E>[]): Result<T[], E> {
  const values: T[] = [];
  for (const result of results) {
    if (!result.ok) {
      return result;
    }
    values.push(result.value);
  }
  return Ok(values);
}
EOF

cat > src/types/index.ts << 'EOF'
// Barrel export for types module
export {
  Priority,
  EventStatus,
  ChannelType,
  type EventKind,
  type Event,
  type EventMetadata,
  type EventSummary,
  isWebhookEvent,
  isSystemEvent,
  createEvent,
} from './event';

export {
  type Result,
  Ok,
  Err,
  unwrap,
  mapResult,
  flatMap,
  collectResults,
} from './result';
EOF

commit "Add core types, interfaces, and generic Result type" "2024-03-02T10:00:00"

# ============================================================
# Commit 3: Configuration module
# ============================================================
mkdir -p src/config

cat > src/config/schema.ts << 'EOF'
import { ChannelType } from '../types';

/** Channel-specific configuration — discriminated union */
export type ChannelConfig =
  | { type: ChannelType.Http; url: string; timeout: number; headers?: Record<string, string> }
  | { type: ChannelType.Slack; webhookUrl: string; channel?: string; username?: string }
  | { type: ChannelType.Email; smtpHost: string; smtpPort: number; from: string; to: string[] };

/** Type guard for Slack channel config */
export function isSlackConfig(config: ChannelConfig): config is ChannelConfig & { type: ChannelType.Slack } {
  return config.type === ChannelType.Slack;
}

/** Type guard for HTTP channel config */
export function isHttpConfig(config: ChannelConfig): config is ChannelConfig & { type: ChannelType.Http } {
  return config.type === ChannelType.Http;
}

/** Retry configuration */
export interface RetryConfig {
  maxAttempts: number;
  initialDelayMs: number;
  maxDelayMs: number;
  backoffMultiplier: number;
}

/** Server configuration */
export interface ServerConfig {
  port: number;
  host: string;
  apiKeyHeader: string;
  apiKeys: string[];
}

/** Top-level application configuration */
export interface AppConfig {
  server: ServerConfig;
  retry: RetryConfig;
  channels: ChannelConfig[];
  logLevel: string;
  enableMetrics: boolean;
}

/** Default configuration values */
export const DEFAULT_CONFIG: AppConfig = {
  server: {
    port: 3000,
    host: '0.0.0.0',
    apiKeyHeader: 'x-api-key',
    apiKeys: [],
  },
  retry: {
    maxAttempts: 5,
    initialDelayMs: 1000,
    maxDelayMs: 60000,
    backoffMultiplier: 2,
  },
  channels: [],
  logLevel: 'info',
  enableMetrics: false,
};
EOF

cat > src/config/loader.ts << 'EOF'
import { AppConfig, ChannelConfig, DEFAULT_CONFIG } from './schema';
import { ChannelType } from '../types';
import { Result, Ok, Err } from '../types/result';

/** Validation error for config issues */
export class ConfigValidationError extends Error {
  constructor(
    public readonly field: string,
    public readonly reason: string
  ) {
    super(`Config validation failed for '${field}': ${reason}`);
    this.name = 'ConfigValidationError';
  }
}

/** Load configuration from environment variables with defaults */
export function loadConfig(env: Record<string, string | undefined> = process.env): Result<AppConfig, ConfigValidationError> {
  const config: AppConfig = {
    ...DEFAULT_CONFIG,
    server: {
      ...DEFAULT_CONFIG.server,
      port: parseInt(env.PORT ?? String(DEFAULT_CONFIG.server.port), 10),
      host: env.HOST ?? DEFAULT_CONFIG.server.host,
      apiKeys: env.API_KEYS ? env.API_KEYS.split(',').map(k => k.trim()) : [],
      apiKeyHeader: env.API_KEY_HEADER ?? DEFAULT_CONFIG.server.apiKeyHeader,
    },
    retry: {
      ...DEFAULT_CONFIG.retry,
      maxAttempts: parseInt(env.RETRY_MAX_ATTEMPTS ?? String(DEFAULT_CONFIG.retry.maxAttempts), 10),
      initialDelayMs: parseInt(env.RETRY_INITIAL_DELAY ?? String(DEFAULT_CONFIG.retry.initialDelayMs), 10),
      maxDelayMs: parseInt(env.RETRY_MAX_DELAY ?? String(DEFAULT_CONFIG.retry.maxDelayMs), 10),
    },
    logLevel: env.LOG_LEVEL ?? DEFAULT_CONFIG.logLevel,
    enableMetrics: env.ENABLE_METRICS === 'true',
  };

  // Build channel configs from environment
  const channels = buildChannelConfigs(env);
  config.channels = channels;

  return validateConfig(config);
}

/** Build channel configurations from environment */
function buildChannelConfigs(env: Record<string, string | undefined>): ChannelConfig[] {
  const channels: ChannelConfig[] = [];

  if (env.SLACK_WEBHOOK_URL) {
    channels.push({
      type: ChannelType.Slack,
      webhookUrl: env.SLACK_WEBHOOK_URL,
      channel: env.SLACK_CHANNEL,
      username: env.SLACK_USERNAME ?? 'WebhookRelay',
    });
  }

  if (env.SMTP_HOST) {
    channels.push({
      type: ChannelType.Email,
      smtpHost: env.SMTP_HOST,
      smtpPort: parseInt(env.SMTP_PORT ?? '587', 10),
      from: env.SMTP_FROM ?? 'webhookrelay@localhost',
      to: (env.SMTP_TO ?? '').split(',').map(s => s.trim()).filter(Boolean),
    });
  }

  if (env.HTTP_FORWARD_URL) {
    channels.push({
      type: ChannelType.Http,
      url: env.HTTP_FORWARD_URL,
      timeout: parseInt(env.HTTP_FORWARD_TIMEOUT ?? '5000', 10),
    });
  }

  return channels;
}

/** Validate the loaded configuration */
function validateConfig(config: AppConfig): Result<AppConfig, ConfigValidationError> {
  if (config.server.port < 1 || config.server.port > 65535) {
    return Err(new ConfigValidationError('server.port', 'must be between 1 and 65535'));
  }
  if (config.retry.maxAttempts < 1) {
    return Err(new ConfigValidationError('retry.maxAttempts', 'must be at least 1'));
  }
  if (config.retry.initialDelayMs < 0) {
    return Err(new ConfigValidationError('retry.initialDelayMs', 'must be non-negative'));
  }
  return Ok(config);
}

/** Merge partial config overrides — uses Partial utility type */
export function mergeConfig(base: AppConfig, overrides: Partial<AppConfig>): AppConfig {
  return {
    ...base,
    ...overrides,
    server: { ...base.server, ...overrides.server },
    retry: { ...base.retry, ...overrides.retry },
    channels: overrides.channels ?? base.channels,
  };
}
EOF

cat > src/config/index.ts << 'EOF'
// Barrel export for config module
export { type AppConfig, type ServerConfig, type RetryConfig, type ChannelConfig, DEFAULT_CONFIG, isSlackConfig, isHttpConfig } from './schema';
export { loadConfig, mergeConfig, ConfigValidationError } from './loader';
EOF

commit "Add configuration module with env loading and validation" "2024-03-04T11:00:00"

# ============================================================
# Commit 4: Channel interface + HTTP channel
# ============================================================
mkdir -p src/channels

cat > src/channels/channel.ts << 'EOF'
import { Event, Result } from '../types';

/** Delivery receipt returned by channels */
export interface DeliveryReceipt {
  channelId: string;
  eventId: string;
  success: boolean;
  timestamp: Date;
  responseCode?: number;
  errorMessage?: string;
  durationMs: number;
}

/**
 * Channel interface — all delivery channels implement this.
 * The send() method name is intentionally repeated across implementations
 * to stress deduplication in the extraction pipeline.
 */
export interface Channel {
  readonly id: string;
  readonly name: string;
  readonly type: string;

  /** Send an event through this channel */
  send(event: Event): Promise<Result<DeliveryReceipt>>;

  /** Check if this channel is healthy */
  healthCheck(): Promise<boolean>;

  /** Get channel-specific configuration summary */
  describe(): string;
}
EOF

cat > src/channels/http-channel.ts << 'EOF'
import { Channel, DeliveryReceipt } from './channel';
import { Event, Result, Ok, Err, ChannelType } from '../types';

export interface HttpChannelOptions {
  url: string;
  timeout: number;
  headers?: Record<string, string>;
  retryOnTimeout?: boolean;
}

/**
 * HTTP Channel — forwards events to a configured HTTP endpoint.
 * Implements the Channel interface with HTTP-specific delivery logic.
 */
export class HttpChannel implements Channel {
  readonly id: string;
  readonly name: string;
  readonly type = ChannelType.Http;

  private readonly url: string;
  private readonly timeout: number;
  private readonly headers: Record<string, string>;

  constructor(id: string, options: HttpChannelOptions) {
    this.id = id;
    this.name = `http-${id}`;
    this.url = options.url;
    this.timeout = options.timeout;
    this.headers = {
      'Content-Type': 'application/json',
      'User-Agent': 'WebhookRelay/1.0',
      ...options.headers,
    };
  }

  async send(event: Event): Promise<Result<DeliveryReceipt>> {
    const start = Date.now();

    try {
      const controller = new AbortController();
      const timeoutId = setTimeout(() => controller.abort(), this.timeout);

      const response = await fetch(this.url, {
        method: 'POST',
        headers: {
          ...this.headers,
          'X-Webhook-Event-Id': event.id,
          'X-Webhook-Event-Type': event.type,
        },
        body: JSON.stringify({
          event: {
            id: event.id,
            type: event.type,
            payload: event.payload,
            metadata: event.metadata,
          },
          timestamp: new Date().toISOString(),
        }),
        signal: controller.signal,
      });

      clearTimeout(timeoutId);
      const durationMs = Date.now() - start;

      if (response.ok) {
        return Ok({
          channelId: this.id,
          eventId: event.id,
          success: true,
          timestamp: new Date(),
          responseCode: response.status,
          durationMs,
        });
      }

      return Err(new Error(`HTTP ${response.status}: ${response.statusText}`));
    } catch (error) {
      const durationMs = Date.now() - start;
      const message = error instanceof Error ? error.message : 'Unknown error';

      return Ok({
        channelId: this.id,
        eventId: event.id,
        success: false,
        timestamp: new Date(),
        errorMessage: message,
        durationMs,
      });
    }
  }

  async healthCheck(): Promise<boolean> {
    try {
      const controller = new AbortController();
      const timeoutId = setTimeout(() => controller.abort(), 3000);
      const response = await fetch(this.url, {
        method: 'HEAD',
        signal: controller.signal,
      });
      clearTimeout(timeoutId);
      return response.ok;
    } catch {
      return false;
    }
  }

  describe(): string {
    return `HttpChannel(${this.id}) -> ${this.url} [timeout=${this.timeout}ms]`;
  }
}
EOF

cat > src/channels/index.ts << 'EOF'
// Barrel export for channels module
export { type Channel, type DeliveryReceipt } from './channel';
export { HttpChannel, type HttpChannelOptions } from './http-channel';
EOF

commit "Add Channel interface and HTTP channel implementation" "2024-03-06T14:00:00"

# ============================================================
# Commit 5: Slack & Email channels + ChannelRegistry
# ============================================================
cat > src/channels/slack-channel.ts << 'EOF'
import { Channel, DeliveryReceipt } from './channel';
import { Event, Result, Ok, Err, ChannelType, Priority } from '../types';

export interface SlackChannelOptions {
  webhookUrl: string;
  channel?: string;
  username?: string;
  iconEmoji?: string;
}

/**
 * Slack Channel — delivers events via Slack incoming webhooks.
 * Formats events as Slack Block Kit messages with priority-based coloring.
 */
export class SlackChannel implements Channel {
  readonly id: string;
  readonly name: string;
  readonly type = ChannelType.Slack;

  private readonly webhookUrl: string;
  private readonly channel?: string;
  private readonly username: string;

  constructor(id: string, options: SlackChannelOptions) {
    this.id = id;
    this.name = `slack-${id}`;
    this.webhookUrl = options.webhookUrl;
    this.channel = options.channel;
    this.username = options.username ?? 'WebhookRelay';
  }

  async send(event: Event): Promise<Result<DeliveryReceipt>> {
    const start = Date.now();

    try {
      const slackPayload = this.formatSlackMessage(event);

      const response = await fetch(this.webhookUrl, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(slackPayload),
      });

      const durationMs = Date.now() - start;

      if (response.ok) {
        return Ok({
          channelId: this.id,
          eventId: event.id,
          success: true,
          timestamp: new Date(),
          responseCode: response.status,
          durationMs,
        });
      }

      return Err(new Error(`Slack API error: ${response.status}`));
    } catch (error) {
      return Err(error instanceof Error ? error : new Error('Slack delivery failed'));
    }
  }

  async healthCheck(): Promise<boolean> {
    // Slack webhooks don't support health checks
    return true;
  }

  describe(): string {
    return `SlackChannel(${this.id}) -> ${this.channel ?? 'default'} as ${this.username}`;
  }

  private formatSlackMessage(event: Event): Record<string, unknown> {
    const color = this.priorityColor(event.priority);

    return {
      channel: this.channel,
      username: this.username,
      attachments: [
        {
          color,
          title: `[${event.type}] Event ${event.id.slice(0, 8)}`,
          text: JSON.stringify(event.payload, null, 2).slice(0, 500),
          fields: [
            { title: 'Source', value: event.metadata.source, short: true },
            { title: 'Priority', value: event.priority, short: true },
            { title: 'Status', value: event.status, short: true },
          ],
          ts: Math.floor(event.createdAt.getTime() / 1000),
        },
      ],
    };
  }

  private priorityColor(priority: Priority): string {
    const colors: Record<Priority, string> = {
      [Priority.Low]: '#36a64f',
      [Priority.Normal]: '#2196f3',
      [Priority.High]: '#ff9800',
      [Priority.Critical]: '#f44336',
    };
    return colors[priority];
  }
}
EOF

cat > src/channels/email-channel.ts << 'EOF'
import { Channel, DeliveryReceipt } from './channel';
import { Event, Result, Ok, Err, ChannelType } from '../types';

export interface EmailChannelOptions {
  smtpHost: string;
  smtpPort: number;
  from: string;
  to: string[];
  secure?: boolean;
}

/**
 * Email Channel — delivers events via SMTP.
 * Formats events as HTML email with structured event data.
 *
 * NOTE: This is a simplified implementation that builds the SMTP payload
 * but delegates actual sending to an injected transport in production.
 */
export class EmailChannel implements Channel {
  readonly id: string;
  readonly name: string;
  readonly type = ChannelType.Email;

  private readonly smtpHost: string;
  private readonly smtpPort: number;
  private readonly from: string;
  private readonly to: string[];

  constructor(id: string, options: EmailChannelOptions) {
    this.id = id;
    this.name = `email-${id}`;
    this.smtpHost = options.smtpHost;
    this.smtpPort = options.smtpPort;
    this.from = options.from;
    this.to = options.to;
  }

  async send(event: Event): Promise<Result<DeliveryReceipt>> {
    const start = Date.now();

    try {
      const emailPayload = this.formatEmail(event);

      // In production, this would use nodemailer or similar.
      // For now, we simulate the send operation.
      await this.simulateSend(emailPayload);

      const durationMs = Date.now() - start;
      return Ok({
        channelId: this.id,
        eventId: event.id,
        success: true,
        timestamp: new Date(),
        durationMs,
      });
    } catch (error) {
      return Err(error instanceof Error ? error : new Error('Email delivery failed'));
    }
  }

  async healthCheck(): Promise<boolean> {
    // Would perform SMTP EHLO in production
    return this.smtpHost.length > 0;
  }

  describe(): string {
    return `EmailChannel(${this.id}) -> ${this.to.join(', ')} via ${this.smtpHost}:${this.smtpPort}`;
  }

  private formatEmail(event: Event): { subject: string; html: string; to: string[]; from: string } {
    return {
      subject: `[WebhookRelay] ${event.type} — ${event.priority} priority`,
      html: `
        <h2>Webhook Event: ${event.type}</h2>
        <p><strong>ID:</strong> ${event.id}</p>
        <p><strong>Source:</strong> ${event.metadata.source}</p>
        <p><strong>Priority:</strong> ${event.priority}</p>
        <pre>${JSON.stringify(event.payload, null, 2)}</pre>
      `,
      to: this.to,
      from: this.from,
    };
  }

  private async simulateSend(_payload: { subject: string; html: string }): Promise<void> {
    // Simulate network delay
    await new Promise(resolve => setTimeout(resolve, 10));
  }
}
EOF

cat > src/channels/registry.ts << 'EOF'
import { Channel } from './channel';
import { ChannelType } from '../types';

/**
 * ChannelRegistry — manages available delivery channels.
 * Supports lookup by ID or type, with registration and removal.
 * Uses the Registry pattern for runtime channel management.
 */
export class ChannelRegistry {
  private channels: Map<string, Channel> = new Map();

  /** Register a channel */
  register(channel: Channel): void {
    if (this.channels.has(channel.id)) {
      throw new Error(`Channel '${channel.id}' is already registered`);
    }
    this.channels.set(channel.id, channel);
  }

  /** Remove a channel by ID */
  unregister(id: string): boolean {
    return this.channels.delete(id);
  }

  /** Get a channel by ID */
  get(id: string): Channel | undefined {
    return this.channels.get(id);
  }

  /** Get all channels of a given type */
  getByType(type: ChannelType): Channel[] {
    return Array.from(this.channels.values()).filter(ch => ch.type === type);
  }

  /** Get all registered channels */
  getAll(): Channel[] {
    return Array.from(this.channels.values());
  }

  /** Check if a channel is registered */
  has(id: string): boolean {
    return this.channels.has(id);
  }

  /** Get count of registered channels */
  get size(): number {
    return this.channels.size;
  }

  /** Run health checks on all channels */
  async healthCheckAll(): Promise<Map<string, boolean>> {
    const results = new Map<string, boolean>();
    const checks = this.getAll().map(async (ch) => {
      const healthy = await ch.healthCheck();
      results.set(ch.id, healthy);
    });
    await Promise.allSettled(checks);
    return results;
  }
}
EOF

# Update barrel export
cat > src/channels/index.ts << 'EOF'
// Barrel export for channels module
export { type Channel, type DeliveryReceipt } from './channel';
export { HttpChannel, type HttpChannelOptions } from './http-channel';
export { SlackChannel, type SlackChannelOptions } from './slack-channel';
export { EmailChannel, type EmailChannelOptions } from './email-channel';
export { ChannelRegistry } from './registry';
EOF

commit "Add Slack and Email channels with ChannelRegistry" "2024-03-08T09:30:00"

# ============================================================
# Commit 6: Filter engine
# ============================================================
mkdir -p src/filters

cat > src/filters/filter.ts << 'EOF'
import { Event, Result } from '../types';

/** Filter verdict with reason */
export interface FilterVerdict {
  pass: boolean;
  filterName: string;
  reason?: string;
}

/**
 * Filter interface — event filters implement this to decide
 * whether an event should proceed through the pipeline.
 */
export interface Filter {
  readonly name: string;

  /** Evaluate an event against this filter */
  evaluate(event: Event): Result<FilterVerdict>;
}

/** Predicate function type for simple filters */
export type FilterPredicate = (event: Event) => boolean;
EOF

cat > src/filters/header-filter.ts << 'EOF'
import { Filter, FilterVerdict } from './filter';
import { Event, Result, Ok, isWebhookEvent } from '../types';

export interface HeaderFilterRule {
  header: string;
  pattern: string;
  negate?: boolean;
}

/**
 * HeaderFilter — filters events based on webhook header values.
 * Only applies to webhook events; system/scheduled events pass through.
 */
export class HeaderFilter implements Filter {
  readonly name: string;
  private readonly rules: HeaderFilterRule[];

  constructor(name: string, rules: HeaderFilterRule[]) {
    this.name = name;
    this.rules = rules;
  }

  evaluate(event: Event): Result<FilterVerdict> {
    if (!isWebhookEvent(event.eventKind)) {
      return Ok({ pass: true, filterName: this.name, reason: 'non-webhook event, skipping header filter' });
    }

    const headers = event.eventKind.headers;

    for (const rule of this.rules) {
      const headerValue = headers[rule.header.toLowerCase()];
      if (headerValue === undefined) {
        if (!rule.negate) {
          return Ok({ pass: false, filterName: this.name, reason: `missing header: ${rule.header}` });
        }
        continue;
      }

      const matches = new RegExp(rule.pattern).test(headerValue);
      const pass = rule.negate ? !matches : matches;

      if (!pass) {
        return Ok({
          pass: false,
          filterName: this.name,
          reason: `header '${rule.header}' ${rule.negate ? 'matched excluded' : 'did not match'} pattern '${rule.pattern}'`,
        });
      }
    }

    return Ok({ pass: true, filterName: this.name });
  }
}
EOF

cat > src/filters/payload-filter.ts << 'EOF'
import { Filter, FilterVerdict, FilterPredicate } from './filter';
import { Event, Result, Ok, Err } from '../types';

/**
 * PayloadFilter — filters events based on payload content.
 * Supports JSONPath-like field access and custom predicates.
 */
export class PayloadFilter implements Filter {
  readonly name: string;
  private readonly predicate: FilterPredicate;

  constructor(name: string, predicate: FilterPredicate) {
    this.name = name;
    this.predicate = predicate;
  }

  evaluate(event: Event): Result<FilterVerdict> {
    try {
      const pass = this.predicate(event);
      return Ok({
        pass,
        filterName: this.name,
        reason: pass ? undefined : 'payload predicate returned false',
      });
    } catch (error) {
      return Err(error instanceof Error ? error : new Error('Payload filter evaluation failed'));
    }
  }

  /** Factory: create a filter that checks for a field's existence */
  static requireField(fieldName: string): PayloadFilter {
    return new PayloadFilter(
      `require-${fieldName}`,
      (event) => fieldName in event.payload
    );
  }

  /** Factory: create a filter that checks a field against a value */
  static fieldEquals(fieldName: string, expected: unknown): PayloadFilter {
    return new PayloadFilter(
      `${fieldName}-equals`,
      (event) => event.payload[fieldName] === expected
    );
  }
}
EOF

cat > src/filters/composite-filter.ts << 'EOF'
import { Filter, FilterVerdict } from './filter';
import { Event, Result, Ok, collectResults } from '../types';

export type CompositeMode = 'all' | 'any' | 'none';

/**
 * CompositeFilter — combines multiple filters with AND/OR/NONE logic.
 * Implements the Composite pattern for building complex filter rules.
 */
export class CompositeFilter implements Filter {
  readonly name: string;
  private readonly filters: Filter[];
  private readonly mode: CompositeMode;

  constructor(name: string, filters: Filter[], mode: CompositeMode = 'all') {
    this.name = name;
    this.filters = filters;
    this.mode = mode;
  }

  evaluate(event: Event): Result<FilterVerdict> {
    const results = this.filters.map(f => f.evaluate(event));
    const collected = collectResults(results);

    if (!collected.ok) {
      return collected;
    }

    const verdicts = collected.value;

    switch (this.mode) {
      case 'all': {
        const failed = verdicts.find(v => !v.pass);
        if (failed) {
          return Ok({ pass: false, filterName: this.name, reason: `sub-filter '${failed.filterName}' failed: ${failed.reason}` });
        }
        return Ok({ pass: true, filterName: this.name });
      }
      case 'any': {
        const passed = verdicts.find(v => v.pass);
        if (passed) {
          return Ok({ pass: true, filterName: this.name });
        }
        return Ok({ pass: false, filterName: this.name, reason: 'no sub-filters passed' });
      }
      case 'none': {
        const passed = verdicts.find(v => v.pass);
        if (passed) {
          return Ok({ pass: false, filterName: this.name, reason: `sub-filter '${passed.filterName}' unexpectedly passed` });
        }
        return Ok({ pass: true, filterName: this.name });
      }
    }
  }

  /** Add a filter to the composite */
  add(filter: Filter): void {
    this.filters.push(filter);
  }

  /** Get count of sub-filters */
  get count(): number {
    return this.filters.length;
  }
}
EOF

cat > src/filters/index.ts << 'EOF'
// Barrel export for filters module
export { type Filter, type FilterVerdict, type FilterPredicate } from './filter';
export { HeaderFilter, type HeaderFilterRule } from './header-filter';
export { PayloadFilter } from './payload-filter';
export { CompositeFilter, type CompositeMode } from './composite-filter';
EOF

commit "Add filter engine with header, payload, and composite filters" "2024-03-10T10:00:00"

# ============================================================
# Commit 7: Retry & queue
# ============================================================
mkdir -p src/retry

cat > src/retry/policy.ts << 'EOF'
/** Retry decision from a policy evaluation */
export interface RetryDecision {
  shouldRetry: boolean;
  delayMs: number;
  reason: string;
}

/**
 * RetryPolicy interface — defines how retry decisions are made.
 * Different implementations provide different backoff strategies.
 */
export interface RetryPolicy {
  readonly name: string;
  readonly maxAttempts: number;

  /** Decide whether to retry and how long to wait */
  evaluate(attempt: number, error?: Error): RetryDecision;

  /** Reset the policy state */
  reset(): void;
}
EOF

cat > src/retry/exponential-backoff.ts << 'EOF'
import { RetryPolicy, RetryDecision } from './policy';

export interface ExponentialBackoffOptions {
  maxAttempts: number;
  initialDelayMs: number;
  maxDelayMs: number;
  multiplier: number;
  jitterFactor?: number;
}

/**
 * ExponentialBackoff — implements exponential backoff with optional jitter.
 * Delay formula: min(initialDelay * multiplier^attempt + jitter, maxDelay)
 */
export class ExponentialBackoff implements RetryPolicy {
  readonly name = 'exponential-backoff';
  readonly maxAttempts: number;

  private readonly initialDelayMs: number;
  private readonly maxDelayMs: number;
  private readonly multiplier: number;
  private readonly jitterFactor: number;

  constructor(options: ExponentialBackoffOptions) {
    this.maxAttempts = options.maxAttempts;
    this.initialDelayMs = options.initialDelayMs;
    this.maxDelayMs = options.maxDelayMs;
    this.multiplier = options.multiplier;
    this.jitterFactor = options.jitterFactor ?? 0.1;
  }

  evaluate(attempt: number, error?: Error): RetryDecision {
    if (attempt >= this.maxAttempts) {
      return {
        shouldRetry: false,
        delayMs: 0,
        reason: `max attempts (${this.maxAttempts}) exceeded`,
      };
    }

    // Check for non-retryable errors
    if (error && this.isNonRetryable(error)) {
      return {
        shouldRetry: false,
        delayMs: 0,
        reason: `non-retryable error: ${error.message}`,
      };
    }

    const baseDelay = this.initialDelayMs * Math.pow(this.multiplier, attempt);
    const jitter = baseDelay * this.jitterFactor * Math.random();
    const delayMs = Math.min(baseDelay + jitter, this.maxDelayMs);

    return {
      shouldRetry: true,
      delayMs: Math.round(delayMs),
      reason: `attempt ${attempt + 1}/${this.maxAttempts}, delay ${Math.round(delayMs)}ms`,
    };
  }

  reset(): void {
    // Stateless — nothing to reset
  }

  private isNonRetryable(error: Error): boolean {
    const nonRetryablePatterns = ['authentication', 'forbidden', '401', '403', '404'];
    const message = error.message.toLowerCase();
    return nonRetryablePatterns.some(pattern => message.includes(pattern));
  }
}
EOF

cat > src/retry/queue.ts << 'EOF'
import { RetryPolicy, RetryDecision } from './policy';

/** Item stored in the retry queue */
export interface QueueItem<T> {
  id: string;
  data: T;
  attempt: number;
  nextRetryAt: Date;
  lastError?: string;
  createdAt: Date;
}

/**
 * Generic in-memory retry queue.
 * Queue<T> provides type-safe storage of items pending retry.
 * Items are dequeued in FIFO order when their retry time has elapsed.
 */
export class InMemoryQueue<T> {
  private items: QueueItem<T>[] = [];
  private readonly policy: RetryPolicy;

  constructor(policy: RetryPolicy) {
    this.policy = policy;
  }

  /** Enqueue an item for retry */
  enqueue(id: string, data: T, error?: Error): RetryDecision {
    const existing = this.items.find(item => item.id === id);
    const attempt = existing ? existing.attempt + 1 : 0;

    const decision = this.policy.evaluate(attempt, error);

    if (!decision.shouldRetry) {
      // Remove from queue if max retries exceeded
      this.items = this.items.filter(item => item.id !== id);
      return decision;
    }

    const nextRetryAt = new Date(Date.now() + decision.delayMs);

    if (existing) {
      existing.attempt = attempt;
      existing.nextRetryAt = nextRetryAt;
      existing.lastError = error?.message;
    } else {
      this.items.push({
        id,
        data,
        attempt,
        nextRetryAt,
        lastError: error?.message,
        createdAt: new Date(),
      });
    }

    return decision;
  }

  /** Dequeue items ready for retry */
  dequeueReady(): QueueItem<T>[] {
    const now = new Date();
    const ready = this.items.filter(item => item.nextRetryAt <= now);
    this.items = this.items.filter(item => item.nextRetryAt > now);
    return ready;
  }

  /** Peek at the next item without removing it */
  peek(): QueueItem<T> | undefined {
    return this.items[0];
  }

  /** Remove a specific item from the queue */
  remove(id: string): boolean {
    const before = this.items.length;
    this.items = this.items.filter(item => item.id !== id);
    return this.items.length < before;
  }

  /** Get current queue depth */
  get size(): number {
    return this.items.length;
  }

  /** Check if queue is empty */
  get isEmpty(): boolean {
    return this.items.length === 0;
  }

  /** Get all items (for inspection) */
  getAll(): ReadonlyArray<QueueItem<T>> {
    return this.items;
  }

  /** Clear the queue */
  clear(): void {
    this.items = [];
  }
}
EOF

cat > src/retry/index.ts << 'EOF'
// Barrel export for retry module
export { type RetryPolicy, type RetryDecision } from './policy';
export { ExponentialBackoff, type ExponentialBackoffOptions } from './exponential-backoff';
export { InMemoryQueue, type QueueItem } from './queue';
EOF

commit "Add retry engine with exponential backoff and generic queue" "2024-03-12T15:00:00"

# ============================================================
# Commit 8: Event router (orchestrator)
# ============================================================
mkdir -p src/router

cat > src/router/router.ts << 'EOF'
import { Event, EventStatus, Result, Ok, Err } from '../types';
import { Channel, DeliveryReceipt, ChannelRegistry } from '../channels';
import { Filter, FilterVerdict } from '../filters';
import { InMemoryQueue } from '../retry';

/** Route configuration — maps event types to channel IDs */
export interface Route {
  eventType: string;
  channelIds: string[];
  filters?: Filter[];
}

/** Result of routing an event */
export interface RoutingResult {
  eventId: string;
  deliveries: DeliveryReceipt[];
  filtered: FilterVerdict[];
  errors: string[];
}

/**
 * EventRouter — orchestrates event delivery through the pipeline.
 *
 * Pipeline stages:
 * 1. Match routes by event type
 * 2. Apply route-specific filters
 * 3. Fan out to matching channels (Promise.allSettled)
 * 4. Enqueue failed deliveries for retry
 *
 * This is the central coordination point of the WebhookRelay system.
 */
export class EventRouter {
  private routes: Map<string, Route> = new Map();
  private readonly registry: ChannelRegistry;
  private readonly retryQueue: InMemoryQueue<{ event: Event; channelId: string }>;
  private globalFilters: Filter[] = [];

  constructor(
    registry: ChannelRegistry,
    retryQueue: InMemoryQueue<{ event: Event; channelId: string }>
  ) {
    this.registry = registry;
    this.retryQueue = retryQueue;
  }

  /** Register a route */
  addRoute(route: Route): void {
    this.routes.set(route.eventType, route);
  }

  /** Remove a route */
  removeRoute(eventType: string): boolean {
    return this.routes.delete(eventType);
  }

  /** Add a global filter applied to all events */
  addGlobalFilter(filter: Filter): void {
    this.globalFilters.push(filter);
  }

  /** Route an event through the pipeline */
  async route(event: Event): Promise<Result<RoutingResult>> {
    const result: RoutingResult = {
      eventId: event.id,
      deliveries: [],
      filtered: [],
      errors: [],
    };

    // Stage 1: Apply global filters
    for (const filter of this.globalFilters) {
      const verdict = filter.evaluate(event);
      if (!verdict.ok) {
        return Err(verdict.error);
      }
      if (!verdict.value.pass) {
        result.filtered.push(verdict.value);
        event.status = EventStatus.Filtered;
        return Ok(result);
      }
    }

    // Stage 2: Find matching routes
    const route = this.routes.get(event.type) ?? this.routes.get('*');
    if (!route) {
      result.errors.push(`no route found for event type '${event.type}'`);
      return Ok(result);
    }

    // Stage 3: Apply route-specific filters
    if (route.filters) {
      for (const filter of route.filters) {
        const verdict = filter.evaluate(event);
        if (!verdict.ok) {
          return Err(verdict.error);
        }
        if (!verdict.value.pass) {
          result.filtered.push(verdict.value);
          event.status = EventStatus.Filtered;
          return Ok(result);
        }
      }
    }

    // Stage 4: Fan out to channels
    event.status = EventStatus.Routing;
    const deliveryPromises = route.channelIds.map(async (channelId) => {
      const channel = this.registry.get(channelId);
      if (!channel) {
        result.errors.push(`channel '${channelId}' not found in registry`);
        return;
      }

      const deliveryResult = await channel.send(event);
      if (deliveryResult.ok) {
        result.deliveries.push(deliveryResult.value);

        // Stage 5: Enqueue failures for retry
        if (!deliveryResult.value.success) {
          this.retryQueue.enqueue(
            `${event.id}:${channelId}`,
            { event, channelId },
            new Error(deliveryResult.value.errorMessage ?? 'delivery failed')
          );
        }
      } else {
        result.errors.push(`channel '${channelId}': ${deliveryResult.error.message}`);
        this.retryQueue.enqueue(
          `${event.id}:${channelId}`,
          { event, channelId },
          deliveryResult.error
        );
      }
    });

    await Promise.allSettled(deliveryPromises);

    // Update event status based on results
    const allSucceeded = result.deliveries.length > 0 && result.deliveries.every(d => d.success);
    event.status = allSucceeded ? EventStatus.Delivered : EventStatus.Failed;

    return Ok(result);
  }

  /** Process the retry queue — called periodically */
  async processRetries(): Promise<DeliveryReceipt[]> {
    const ready = this.retryQueue.dequeueReady();
    const receipts: DeliveryReceipt[] = [];

    for (const item of ready) {
      const channel = this.registry.get(item.data.channelId);
      if (!channel) continue;

      item.data.event.metadata.retryCount = item.attempt;
      item.data.event.status = EventStatus.Retrying;

      const result = await channel.send(item.data.event);
      if (result.ok) {
        receipts.push(result.value);
        if (!result.value.success) {
          this.retryQueue.enqueue(
            item.id,
            item.data,
            new Error(result.value.errorMessage ?? 'retry failed')
          );
        }
      }
    }

    return receipts;
  }

  /** Get all registered routes */
  getRoutes(): Route[] {
    return Array.from(this.routes.values());
  }

  /** Get retry queue depth */
  get pendingRetries(): number {
    return this.retryQueue.size;
  }
}
EOF

cat > src/router/index.ts << 'EOF'
// Barrel export for router module
export { EventRouter, type Route, type RoutingResult } from './router';
EOF

commit "Add EventRouter orchestrator with fan-out and retry integration" "2024-03-14T11:00:00"

# ============================================================
# Commit 9: Storage layer with generic Repository
# ============================================================
mkdir -p src/storage

cat > src/storage/repository.ts << 'EOF'
/**
 * Generic Repository interface — defines CRUD operations for any entity type.
 * Repository<T> is the primary abstraction for data access in WebhookRelay.
 */
export interface Repository<T> {
  /** Find an entity by ID */
  findById(id: string): Promise<T | undefined>;

  /** Find all entities matching a predicate */
  findAll(predicate?: (item: T) => boolean): Promise<T[]>;

  /** Save an entity (insert or update) */
  save(id: string, entity: T): Promise<void>;

  /** Delete an entity by ID */
  delete(id: string): Promise<boolean>;

  /** Count entities, optionally filtered */
  count(predicate?: (item: T) => boolean): Promise<number>;

  /** Check if an entity exists */
  exists(id: string): Promise<boolean>;
}

/**
 * Paginated query result.
 * Uses generics for type-safe pagination across any entity.
 */
export interface Page<T> {
  items: T[];
  total: number;
  offset: number;
  limit: number;
  hasMore: boolean;
}

/** Query options for repository operations */
export interface QueryOptions {
  offset?: number;
  limit?: number;
  sortBy?: string;
  sortOrder?: 'asc' | 'desc';
}
EOF

cat > src/storage/in-memory-repo.ts << 'EOF'
import { Repository, Page, QueryOptions } from './repository';

/**
 * InMemoryRepository — generic in-memory implementation of Repository<T>.
 * Suitable for development and testing. Stores entities in a Map.
 */
export class InMemoryRepository<T> implements Repository<T> {
  private store: Map<string, T> = new Map();

  async findById(id: string): Promise<T | undefined> {
    return this.store.get(id);
  }

  async findAll(predicate?: (item: T) => boolean): Promise<T[]> {
    const items = Array.from(this.store.values());
    if (predicate) {
      return items.filter(predicate);
    }
    return items;
  }

  async save(id: string, entity: T): Promise<void> {
    this.store.set(id, entity);
  }

  async delete(id: string): Promise<boolean> {
    return this.store.delete(id);
  }

  async count(predicate?: (item: T) => boolean): Promise<number> {
    if (predicate) {
      return Array.from(this.store.values()).filter(predicate).length;
    }
    return this.store.size;
  }

  async exists(id: string): Promise<boolean> {
    return this.store.has(id);
  }

  /** Paginated query — not part of base Repository interface */
  async findPaginated(options: QueryOptions = {}, predicate?: (item: T) => boolean): Promise<Page<T>> {
    const { offset = 0, limit = 50 } = options;
    const all = await this.findAll(predicate);
    const items = all.slice(offset, offset + limit);

    return {
      items,
      total: all.length,
      offset,
      limit,
      hasMore: offset + limit < all.length,
    };
  }

  /** Clear all stored entities */
  async clear(): Promise<void> {
    this.store.clear();
  }
}
EOF

cat > src/storage/event-log.ts << 'EOF'
import { Event, EventStatus, EventSummary } from '../types';
import { InMemoryRepository } from './in-memory-repo';
import { Page, QueryOptions } from './repository';

/** Event log entry with delivery metadata */
export interface EventLogEntry {
  event: Event;
  deliveredTo: string[];
  filteredBy: string[];
  errors: string[];
  completedAt?: Date;
}

/**
 * EventLogRepository — specialized repository for event audit logging.
 * Extends InMemoryRepository with event-specific query methods.
 */
export class EventLogRepository extends InMemoryRepository<EventLogEntry> {
  /** Log a new event */
  async logEvent(event: Event): Promise<void> {
    await this.save(event.id, {
      event,
      deliveredTo: [],
      filteredBy: [],
      errors: [],
    });
  }

  /** Record a successful delivery */
  async recordDelivery(eventId: string, channelId: string): Promise<void> {
    const entry = await this.findById(eventId);
    if (entry) {
      entry.deliveredTo.push(channelId);
      await this.save(eventId, entry);
    }
  }

  /** Record a filter rejection */
  async recordFilter(eventId: string, filterName: string): Promise<void> {
    const entry = await this.findById(eventId);
    if (entry) {
      entry.filteredBy.push(filterName);
      await this.save(eventId, entry);
    }
  }

  /** Record an error */
  async recordError(eventId: string, error: string): Promise<void> {
    const entry = await this.findById(eventId);
    if (entry) {
      entry.errors.push(error);
      await this.save(eventId, entry);
    }
  }

  /** Find events by status */
  async findByStatus(status: EventStatus): Promise<EventLogEntry[]> {
    return this.findAll(entry => entry.event.status === status);
  }

  /** Get event summaries with pagination */
  async getSummaries(options: QueryOptions = {}): Promise<Page<EventSummary>> {
    const page = await this.findPaginated(options);
    return {
      ...page,
      items: page.items.map(entry => ({
        id: entry.event.id,
        type: entry.event.type,
        status: entry.event.status,
        priority: entry.event.priority,
        createdAt: entry.event.createdAt,
      })),
    };
  }

  /** Get events from the last N minutes */
  async getRecent(minutes: number): Promise<EventLogEntry[]> {
    const cutoff = new Date(Date.now() - minutes * 60 * 1000);
    return this.findAll(entry => entry.event.createdAt >= cutoff);
  }
}
EOF

cat > src/storage/index.ts << 'EOF'
// Barrel export for storage module
export { type Repository, type Page, type QueryOptions } from './repository';
export { InMemoryRepository } from './in-memory-repo';
export { EventLogRepository, type EventLogEntry } from './event-log';
EOF

commit "Add storage layer with generic Repository and EventLogRepository" "2024-03-16T13:00:00"

# ============================================================
# Commit 10: Express API + middleware
# ============================================================
mkdir -p src/api

cat > src/api/middleware.ts << 'EOF'
import { Request, Response, NextFunction } from 'express';
import { ServerConfig } from '../config';

/** Request with parsed webhook data */
export interface WebhookRequest extends Request {
  webhookSource?: string;
  requestId?: string;
  authenticatedKey?: string;
}

/**
 * Authentication middleware — validates API key from configured header.
 * Skips auth if no API keys are configured (development mode).
 */
export function authMiddleware(config: ServerConfig) {
  return (req: WebhookRequest, res: Response, next: NextFunction): void => {
    // Skip auth if no keys configured
    if (config.apiKeys.length === 0) {
      next();
      return;
    }

    const apiKey = req.headers[config.apiKeyHeader.toLowerCase()] as string | undefined;

    if (!apiKey) {
      res.status(401).json({
        error: 'Unauthorized',
        message: `Missing ${config.apiKeyHeader} header`,
      });
      return;
    }

    if (!config.apiKeys.includes(apiKey)) {
      res.status(403).json({
        error: 'Forbidden',
        message: 'Invalid API key',
      });
      return;
    }

    req.authenticatedKey = apiKey;
    next();
  };
}

/**
 * Request ID middleware — assigns a unique ID to each request.
 * Checks for existing X-Request-Id header first.
 */
export function requestIdMiddleware() {
  let counter = 0;
  return (req: WebhookRequest, _res: Response, next: NextFunction): void => {
    req.requestId = (req.headers['x-request-id'] as string) ?? `req-${Date.now()}-${++counter}`;
    next();
  };
}

/**
 * Request logging middleware — logs method, path, status, and duration.
 */
export function loggingMiddleware() {
  return (req: Request, res: Response, next: NextFunction): void => {
    const start = Date.now();

    res.on('finish', () => {
      const duration = Date.now() - start;
      const level = res.statusCode >= 400 ? 'warn' : 'info';
      console[level === 'warn' ? 'warn' : 'log'](
        `${req.method} ${req.path} ${res.statusCode} ${duration}ms`
      );
    });

    next();
  };
}

/**
 * Error handler middleware — catches unhandled errors and returns JSON.
 */
export function errorHandler() {
  return (err: Error, _req: Request, res: Response, _next: NextFunction): void => {
    console.error('Unhandled error:', err.message);

    const statusCode = 'statusCode' in err ? (err as Error & { statusCode: number }).statusCode : 500;

    res.status(statusCode).json({
      error: err.name || 'InternalError',
      message: err.message,
      ...(process.env.NODE_ENV === 'development' && { stack: err.stack }),
    });
  };
}

/** Validation error for request body issues */
export class ValidationError extends Error {
  readonly statusCode = 400;

  constructor(message: string) {
    super(message);
    this.name = 'ValidationError';
  }
}
EOF

cat > src/api/routes.ts << 'EOF'
import { Router, Request, Response } from 'express';
import { Event, createEvent, Priority, EventKind, EventStatus } from '../types';
import { EventRouter, RoutingResult } from '../router';
import { EventLogRepository } from '../storage';
import { ChannelRegistry } from '../channels';
import { ValidationError, WebhookRequest } from './middleware';

/** Dependencies injected into route handlers */
export interface RouteDependencies {
  eventRouter: EventRouter;
  eventLog: EventLogRepository;
  channelRegistry: ChannelRegistry;
}

/**
 * Create the webhook ingestion routes.
 * POST /webhooks/:source — receive and process a webhook event.
 */
export function createWebhookRoutes(deps: RouteDependencies): Router {
  const router = Router();

  // Receive webhook event
  router.post('/webhooks/:source', async (req: WebhookRequest, res: Response) => {
    const source = req.params.source;
    if (!source) {
      throw new ValidationError('Missing source parameter');
    }

    const body = req.body as Record<string, unknown>;
    if (!body || typeof body !== 'object') {
      throw new ValidationError('Request body must be a JSON object');
    }

    const eventType = (req.headers['x-event-type'] as string) ?? body.type as string ?? 'unknown';
    const priority = parsePriority(req.headers['x-priority'] as string);

    const eventKind: EventKind = {
      kind: 'webhook',
      source,
      headers: flattenHeaders(req.headers),
    };

    const event = createEvent(eventType, body, eventKind, priority);

    // Log the event
    await deps.eventLog.logEvent(event);

    // Route the event
    const result = await deps.eventRouter.route(event);

    if (!result.ok) {
      await deps.eventLog.recordError(event.id, result.error.message);
      res.status(500).json({ error: 'Routing failed', message: result.error.message });
      return;
    }

    // Update log with delivery results
    for (const delivery of result.value.deliveries) {
      if (delivery.success) {
        await deps.eventLog.recordDelivery(event.id, delivery.channelId);
      }
    }
    for (const verdict of result.value.filtered) {
      await deps.eventLog.recordFilter(event.id, verdict.filterName);
    }

    res.status(202).json({
      eventId: event.id,
      status: event.status,
      deliveries: result.value.deliveries.length,
      filtered: result.value.filtered.length,
      errors: result.value.errors,
    });
  });

  return router;
}

/**
 * Create the admin API routes.
 * GET /admin/events — list events
 * GET /admin/events/:id — get event details
 * GET /admin/channels — list channels
 * GET /admin/health — health check
 */
export function createAdminRoutes(deps: RouteDependencies): Router {
  const router = Router();

  // List events with pagination
  router.get('/admin/events', async (_req: Request, res: Response) => {
    const page = await deps.eventLog.getSummaries({ limit: 50 });
    res.json(page);
  });

  // Get event details
  router.get('/admin/events/:id', async (req: Request, res: Response) => {
    const entry = await deps.eventLog.findById(req.params.id ?? '');
    if (!entry) {
      res.status(404).json({ error: 'Event not found' });
      return;
    }
    res.json(entry);
  });

  // List channels with health status
  router.get('/admin/channels', async (_req: Request, res: Response) => {
    const channels = deps.channelRegistry.getAll();
    const health = await deps.channelRegistry.healthCheckAll();

    res.json(channels.map(ch => ({
      id: ch.id,
      name: ch.name,
      type: ch.type,
      description: ch.describe(),
      healthy: health.get(ch.id) ?? false,
    })));
  });

  // Health check
  router.get('/admin/health', async (_req: Request, res: Response) => {
    const channelHealth = await deps.channelRegistry.healthCheckAll();
    const allHealthy = Array.from(channelHealth.values()).every(h => h);

    const recentEvents = await deps.eventLog.getRecent(5);
    const failedCount = recentEvents.filter(e => e.event.status === EventStatus.Failed).length;

    res.json({
      status: allHealthy && failedCount === 0 ? 'healthy' : 'degraded',
      channels: Object.fromEntries(channelHealth),
      recentEvents: recentEvents.length,
      recentFailures: failedCount,
      uptime: process.uptime(),
    });
  });

  // Get routes
  router.get('/admin/routes', (_req: Request, res: Response) => {
    const routes = deps.eventRouter.getRoutes();
    res.json(routes.map(r => ({
      eventType: r.eventType,
      channelIds: r.channelIds,
      filterCount: r.filters?.length ?? 0,
    })));
  });

  return router;
}

/** Parse priority from header value */
function parsePriority(value?: string): Priority {
  if (!value) return Priority.Normal;
  const normalized = value.toLowerCase();
  const priorities: Record<string, Priority> = {
    low: Priority.Low,
    normal: Priority.Normal,
    high: Priority.High,
    critical: Priority.Critical,
  };
  return priorities[normalized] ?? Priority.Normal;
}

/** Flatten IncomingHttpHeaders to Record<string, string> */
function flattenHeaders(headers: Record<string, string | string[] | undefined>): Record<string, string> {
  const flat: Record<string, string> = {};
  for (const [key, value] of Object.entries(headers)) {
    if (value !== undefined) {
      flat[key] = Array.isArray(value) ? value.join(', ') : value;
    }
  }
  return flat;
}
EOF

cat > src/api/index.ts << 'EOF'
// Barrel export for api module
export {
  authMiddleware,
  requestIdMiddleware,
  loggingMiddleware,
  errorHandler,
  ValidationError,
  type WebhookRequest,
} from './middleware';
export {
  createWebhookRoutes,
  createAdminRoutes,
  type RouteDependencies,
} from './routes';
EOF

commit "Add Express API with middleware, webhook routes, and admin endpoints" "2024-03-18T10:00:00"

# ============================================================
# Commit 11: Main entry point + DI wiring
# ============================================================
cat > src/index.ts << 'EOF'
import express from 'express';
import { loadConfig, AppConfig, ChannelConfig, isSlackConfig, isHttpConfig } from './config';
import { unwrap, ChannelType } from './types';
import { ChannelRegistry, HttpChannel, SlackChannel, EmailChannel, Channel } from './channels';
import { ExponentialBackoff, InMemoryQueue } from './retry';
import { EventRouter } from './router';
import { EventLogRepository } from './storage';
import {
  authMiddleware,
  requestIdMiddleware,
  loggingMiddleware,
  errorHandler,
  createWebhookRoutes,
  createAdminRoutes,
} from './api';

/**
 * Application container — manual dependency injection.
 * Wires together all components and manages lifecycle.
 */
class Application {
  private readonly config: AppConfig;
  private readonly channelRegistry: ChannelRegistry;
  private readonly eventRouter: EventRouter;
  private readonly eventLog: EventLogRepository;
  private retryInterval?: ReturnType<typeof setInterval>;

  constructor(config: AppConfig) {
    this.config = config;
    this.channelRegistry = new ChannelRegistry();
    this.eventLog = new EventLogRepository();

    // Build retry infrastructure
    const retryPolicy = new ExponentialBackoff({
      maxAttempts: config.retry.maxAttempts,
      initialDelayMs: config.retry.initialDelayMs,
      maxDelayMs: config.retry.maxDelayMs,
      multiplier: config.retry.backoffMultiplier,
    });
    const retryQueue = new InMemoryQueue<{ event: import('./types').Event; channelId: string }>(retryPolicy);

    this.eventRouter = new EventRouter(this.channelRegistry, retryQueue);

    // Register channels from config
    this.registerChannels(config.channels);

    // Register default catch-all route
    this.eventRouter.addRoute({
      eventType: '*',
      channelIds: this.channelRegistry.getAll().map(ch => ch.id),
    });
  }

  /** Register channels from configuration */
  private registerChannels(channelConfigs: ChannelConfig[]): void {
    for (const channelConfig of channelConfigs) {
      const channel = this.createChannel(channelConfig);
      if (channel) {
        this.channelRegistry.register(channel);
      }
    }
  }

  /** Factory method — creates a Channel from config */
  private createChannel(config: ChannelConfig): Channel | null {
    switch (config.type) {
      case ChannelType.Http:
        return new HttpChannel(`http-${Date.now()}`, {
          url: config.url,
          timeout: config.timeout,
          headers: config.headers,
        });

      case ChannelType.Slack:
        return new SlackChannel(`slack-${Date.now()}`, {
          webhookUrl: config.webhookUrl,
          channel: config.channel,
          username: config.username,
        });

      case ChannelType.Email:
        return new EmailChannel(`email-${Date.now()}`, {
          smtpHost: config.smtpHost,
          smtpPort: config.smtpPort,
          from: config.from,
          to: config.to,
        });

      default:
        console.warn(`Unknown channel type: ${(config as ChannelConfig).type}`);
        return null;
    }
  }

  /** Start the application */
  start(): void {
    const app = express();

    // Core middleware
    app.use(express.json({ limit: '1mb' }));
    app.use(requestIdMiddleware());
    app.use(loggingMiddleware());

    // Auth middleware for webhook routes
    app.use('/webhooks', authMiddleware(this.config.server));

    // Routes
    const deps = {
      eventRouter: this.eventRouter,
      eventLog: this.eventLog,
      channelRegistry: this.channelRegistry,
    };
    app.use(createWebhookRoutes(deps));
    app.use(createAdminRoutes(deps));

    // Error handler (must be last)
    app.use(errorHandler());

    // Start retry processor
    this.retryInterval = setInterval(async () => {
      try {
        const receipts = await this.eventRouter.processRetries();
        if (receipts.length > 0) {
          console.log(`Processed ${receipts.length} retries`);
        }
      } catch (error) {
        console.error('Retry processing error:', error);
      }
    }, 10000);

    // Start server
    const { port, host } = this.config.server;
    app.listen(port, host, () => {
      console.log(`WebhookRelay listening on ${host}:${port}`);
      console.log(`Channels: ${this.channelRegistry.size}`);
      console.log(`Routes: ${this.eventRouter.getRoutes().length}`);
    });
  }

  /** Graceful shutdown */
  stop(): void {
    if (this.retryInterval) {
      clearInterval(this.retryInterval);
    }
    console.log('WebhookRelay shutting down');
  }
}

// --- Bootstrap ---
function main(): void {
  const configResult = loadConfig();
  const config = unwrap(configResult);

  const app = new Application(config);

  process.on('SIGTERM', () => app.stop());
  process.on('SIGINT', () => app.stop());

  app.start();
}

main();
EOF

commit "Add main entry point with manual dependency injection" "2024-03-20T09:00:00"

# ============================================================
# Commit 12: Test suite
# ============================================================
mkdir -p tests

cat > tests/channels.test.ts << 'EOF'
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { HttpChannel } from '../src/channels/http-channel';
import { SlackChannel } from '../src/channels/slack-channel';
import { EmailChannel } from '../src/channels/email-channel';
import { ChannelRegistry } from '../src/channels/registry';
import { createEvent, Priority, ChannelType } from '../src/types';

describe('HttpChannel', () => {
  const mockEvent = createEvent(
    'test.event',
    { message: 'hello' },
    { kind: 'webhook', source: 'test', headers: {} },
    Priority.Normal
  );

  it('should have correct properties', () => {
    const channel = new HttpChannel('test-http', {
      url: 'https://example.com/webhook',
      timeout: 5000,
    });
    expect(channel.id).toBe('test-http');
    expect(channel.type).toBe(ChannelType.Http);
    expect(channel.describe()).toContain('example.com');
  });

  it('should send events and return delivery receipt', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      statusText: 'OK',
    });

    const channel = new HttpChannel('test-http', {
      url: 'https://example.com/webhook',
      timeout: 5000,
    });

    const result = await channel.send(mockEvent);
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.value.success).toBe(true);
      expect(result.value.responseCode).toBe(200);
    }
  });

  it('should handle fetch errors gracefully', async () => {
    global.fetch = vi.fn().mockRejectedValue(new Error('Network error'));

    const channel = new HttpChannel('test-http', {
      url: 'https://example.com/webhook',
      timeout: 5000,
    });

    const result = await channel.send(mockEvent);
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.value.success).toBe(false);
      expect(result.value.errorMessage).toBe('Network error');
    }
  });
});

describe('SlackChannel', () => {
  it('should format messages with priority colors', async () => {
    let capturedBody: string | undefined;
    global.fetch = vi.fn().mockImplementation((_url: string, init: RequestInit) => {
      capturedBody = init.body as string;
      return Promise.resolve({ ok: true, status: 200 });
    });

    const channel = new SlackChannel('test-slack', {
      webhookUrl: 'https://hooks.slack.com/test',
      channel: '#alerts',
    });

    const event = createEvent(
      'deploy.failed',
      { service: 'api' },
      { kind: 'webhook', source: 'ci', headers: {} },
      Priority.Critical
    );

    await channel.send(event);
    expect(capturedBody).toBeDefined();
    const parsed = JSON.parse(capturedBody!);
    expect(parsed.attachments[0].color).toBe('#f44336'); // Critical = red
  });
});

describe('EmailChannel', () => {
  it('should describe itself with recipient list', () => {
    const channel = new EmailChannel('test-email', {
      smtpHost: 'smtp.example.com',
      smtpPort: 587,
      from: 'relay@example.com',
      to: ['admin@example.com', 'ops@example.com'],
    });
    expect(channel.describe()).toContain('admin@example.com');
    expect(channel.describe()).toContain('ops@example.com');
  });
});

describe('ChannelRegistry', () => {
  let registry: ChannelRegistry;

  beforeEach(() => {
    registry = new ChannelRegistry();
  });

  it('should register and retrieve channels', () => {
    const channel = new HttpChannel('ch1', { url: 'https://example.com', timeout: 1000 });
    registry.register(channel);
    expect(registry.get('ch1')).toBe(channel);
    expect(registry.size).toBe(1);
  });

  it('should prevent duplicate registration', () => {
    const channel = new HttpChannel('ch1', { url: 'https://example.com', timeout: 1000 });
    registry.register(channel);
    expect(() => registry.register(channel)).toThrow('already registered');
  });

  it('should filter channels by type', () => {
    registry.register(new HttpChannel('h1', { url: 'https://a.com', timeout: 1000 }));
    registry.register(new HttpChannel('h2', { url: 'https://b.com', timeout: 1000 }));
    registry.register(new SlackChannel('s1', { webhookUrl: 'https://hooks.slack.com/x' }));

    expect(registry.getByType(ChannelType.Http)).toHaveLength(2);
    expect(registry.getByType(ChannelType.Slack)).toHaveLength(1);
    expect(registry.getByType(ChannelType.Email)).toHaveLength(0);
  });
});
EOF

cat > tests/filters.test.ts << 'EOF'
import { describe, it, expect } from 'vitest';
import { HeaderFilter } from '../src/filters/header-filter';
import { PayloadFilter } from '../src/filters/payload-filter';
import { CompositeFilter } from '../src/filters/composite-filter';
import { createEvent, Priority } from '../src/types';

const makeWebhookEvent = (headers: Record<string, string>, payload: Record<string, unknown> = {}) =>
  createEvent(
    'test',
    payload,
    { kind: 'webhook', source: 'test', headers },
    Priority.Normal
  );

const makeSystemEvent = () =>
  createEvent(
    'test',
    {},
    { kind: 'system', component: 'scheduler' },
    Priority.Normal
  );

describe('HeaderFilter', () => {
  it('should pass events matching header pattern', () => {
    const filter = new HeaderFilter('content-type', [
      { header: 'content-type', pattern: 'application/json' },
    ]);
    const event = makeWebhookEvent({ 'content-type': 'application/json' });
    const result = filter.evaluate(event);
    expect(result.ok).toBe(true);
    if (result.ok) expect(result.value.pass).toBe(true);
  });

  it('should reject events missing required headers', () => {
    const filter = new HeaderFilter('require-auth', [
      { header: 'authorization', pattern: 'Bearer .+' },
    ]);
    const event = makeWebhookEvent({});
    const result = filter.evaluate(event);
    expect(result.ok).toBe(true);
    if (result.ok) expect(result.value.pass).toBe(false);
  });

  it('should skip non-webhook events', () => {
    const filter = new HeaderFilter('any', [
      { header: 'x-custom', pattern: '.*' },
    ]);
    const event = makeSystemEvent();
    const result = filter.evaluate(event);
    expect(result.ok).toBe(true);
    if (result.ok) expect(result.value.pass).toBe(true);
  });

  it('should support negated rules', () => {
    const filter = new HeaderFilter('block-bots', [
      { header: 'user-agent', pattern: 'bot', negate: true },
    ]);

    const botEvent = makeWebhookEvent({ 'user-agent': 'my-bot-crawler' });
    const humanEvent = makeWebhookEvent({ 'user-agent': 'Mozilla/5.0' });

    const botResult = filter.evaluate(botEvent);
    const humanResult = filter.evaluate(humanEvent);

    expect(botResult.ok && botResult.value.pass).toBe(false);
    expect(humanResult.ok && humanResult.value.pass).toBe(true);
  });
});

describe('PayloadFilter', () => {
  it('should filter by field existence', () => {
    const filter = PayloadFilter.requireField('action');
    const withField = makeWebhookEvent({}, { action: 'push' });
    const withoutField = makeWebhookEvent({}, { other: 'value' });

    expect(filter.evaluate(withField).ok && filter.evaluate(withField)).toBeTruthy();
    const result = filter.evaluate(withoutField);
    expect(result.ok && !result.value.pass).toBe(true);
  });

  it('should filter by field value', () => {
    const filter = PayloadFilter.fieldEquals('action', 'push');
    const matching = makeWebhookEvent({}, { action: 'push' });
    const nonMatching = makeWebhookEvent({}, { action: 'pull' });

    const matchResult = filter.evaluate(matching);
    const nonMatchResult = filter.evaluate(nonMatching);

    expect(matchResult.ok && matchResult.value.pass).toBe(true);
    expect(nonMatchResult.ok && nonMatchResult.value.pass).toBe(false);
  });
});

describe('CompositeFilter', () => {
  it('should require all sub-filters to pass in "all" mode', () => {
    const filter = new CompositeFilter('all-pass', [
      PayloadFilter.requireField('action'),
      PayloadFilter.fieldEquals('action', 'push'),
    ], 'all');

    const event = makeWebhookEvent({}, { action: 'push' });
    const result = filter.evaluate(event);
    expect(result.ok && result.value.pass).toBe(true);
  });

  it('should pass if any sub-filter passes in "any" mode', () => {
    const filter = new CompositeFilter('any-pass', [
      PayloadFilter.fieldEquals('action', 'push'),
      PayloadFilter.fieldEquals('action', 'merge'),
    ], 'any');

    const event = makeWebhookEvent({}, { action: 'merge' });
    const result = filter.evaluate(event);
    expect(result.ok && result.value.pass).toBe(true);
  });

  it('should pass only if no sub-filters pass in "none" mode', () => {
    const filter = new CompositeFilter('none-pass', [
      PayloadFilter.fieldEquals('action', 'delete'),
      PayloadFilter.fieldEquals('action', 'force-push'),
    ], 'none');

    const safeEvent = makeWebhookEvent({}, { action: 'push' });
    const dangerousEvent = makeWebhookEvent({}, { action: 'delete' });

    expect(filter.evaluate(safeEvent).ok).toBe(true);
    const dangerResult = filter.evaluate(dangerousEvent);
    expect(dangerResult.ok && dangerResult.value.pass).toBe(false);
  });
});
EOF

cat > tests/router.test.ts << 'EOF'
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { EventRouter, Route } from '../src/router';
import { ChannelRegistry, HttpChannel, DeliveryReceipt } from '../src/channels';
import { ExponentialBackoff, InMemoryQueue } from '../src/retry';
import { PayloadFilter } from '../src/filters';
import { createEvent, Priority, Event, Ok } from '../src/types';

describe('EventRouter', () => {
  let registry: ChannelRegistry;
  let router: EventRouter;

  beforeEach(() => {
    registry = new ChannelRegistry();
    const policy = new ExponentialBackoff({
      maxAttempts: 3,
      initialDelayMs: 100,
      maxDelayMs: 1000,
      multiplier: 2,
    });
    const queue = new InMemoryQueue<{ event: Event; channelId: string }>(policy);
    router = new EventRouter(registry, queue);
  });

  it('should route events to matching channels', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: 'OK' });

    const channel = new HttpChannel('ch1', { url: 'https://example.com', timeout: 1000 });
    registry.register(channel);

    router.addRoute({ eventType: 'push', channelIds: ['ch1'] });

    const event = createEvent(
      'push',
      { ref: 'main' },
      { kind: 'webhook', source: 'github', headers: {} },
      Priority.Normal
    );

    const result = await router.route(event);
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.value.deliveries).toHaveLength(1);
    }
  });

  it('should apply route filters before delivery', async () => {
    const channel = new HttpChannel('ch1', { url: 'https://example.com', timeout: 1000 });
    registry.register(channel);

    router.addRoute({
      eventType: 'push',
      channelIds: ['ch1'],
      filters: [PayloadFilter.fieldEquals('action', 'push')],
    });

    const wrongAction = createEvent(
      'push',
      { action: 'pull' },
      { kind: 'webhook', source: 'github', headers: {} },
      Priority.Normal
    );

    const result = await router.route(wrongAction);
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.value.filtered).toHaveLength(1);
      expect(result.value.deliveries).toHaveLength(0);
    }
  });

  it('should use wildcard route as fallback', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, statusText: 'OK' });

    const channel = new HttpChannel('catch-all', { url: 'https://example.com', timeout: 1000 });
    registry.register(channel);

    router.addRoute({ eventType: '*', channelIds: ['catch-all'] });

    const event = createEvent(
      'unknown.event.type',
      {},
      { kind: 'webhook', source: 'test', headers: {} },
      Priority.Low
    );

    const result = await router.route(event);
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.value.deliveries).toHaveLength(1);
    }
  });

  it('should report error when no route matches', async () => {
    const event = createEvent(
      'unmapped.event',
      {},
      { kind: 'webhook', source: 'test', headers: {} },
      Priority.Normal
    );

    const result = await router.route(event);
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.value.errors).toHaveLength(1);
      expect(result.value.errors[0]).toContain('no route found');
    }
  });
});
EOF

cat > vitest.config.ts << 'EOF'
import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    globals: true,
    environment: 'node',
    include: ['tests/**/*.test.ts'],
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json-summary'],
      include: ['src/**/*.ts'],
      exclude: ['src/index.ts'],
    },
  },
});
EOF

commit "Add test suite with channel, filter, and router tests" "2024-03-22T14:00:00"

# ============================================================
# Commit 13: Tech debt audit + ARCHITECTURE.md
# ============================================================

# Add TODO/FIXME/NOTE comments throughout the codebase

# event.ts — add NOTE about discriminated union
sed -i '' '1i\
// NOTE: Event types use discriminated unions for type-safe event handling.\
// The eventKind field determines the shape of the event data.\
' src/types/event.ts

# http-channel.ts — add TODO
sed -i '' '/class HttpChannel/a\
  // TODO: Add circuit breaker pattern for failing endpoints\
  // TODO: Support configurable retry-on-timeout behavior\
' src/channels/http-channel.ts

# slack-channel.ts — add FIXME
sed -i '' '/class SlackChannel/a\
  // FIXME: Rate limiting not implemented — Slack has 1 msg/sec limit\
  // TODO: Add Block Kit builder for richer message formatting\
' src/channels/slack-channel.ts

# email-channel.ts — add TODO
sed -i '' '/simulateSend/i\
  // TODO: Replace simulated send with nodemailer transport\
  // FIXME: No connection pooling for SMTP connections\
' src/channels/email-channel.ts

# router.ts — add NOTE
sed -i '' '/class EventRouter/a\
  // NOTE: Routes are matched by exact event type, then fallback to wildcard.\
  // TODO: Support glob/regex patterns for route matching (e.g. "deploy.*")\
  // FIXME: Race condition possible when modifying routes during event processing\
' src/router/router.ts

# queue.ts — add TODO
sed -i '' '/class InMemoryQueue/a\
  // TODO: Add persistent queue backed by Redis or SQLite\
  // NOTE: Items are dequeued in insertion order, not by priority\
' src/retry/queue.ts

# in-memory-repo.ts — add TODO
sed -i '' '/class InMemoryRepository/a\
  // TODO: Implement TTL-based expiration for stored entities\
  // TODO: Add change notification (observer pattern) for reactive updates\
' src/storage/in-memory-repo.ts

# middleware.ts — add FIXME
sed -i '' '/class ValidationError/i\
// FIXME: Auth middleware does not support key rotation or expiry\
// TODO: Add rate limiting middleware (token bucket algorithm)\
' src/api/middleware.ts

# index.ts — add NOTE
sed -i '' '/class Application/a\
  // NOTE: This uses manual DI — consider InversifyJS for larger codebases\
  // TODO: Add graceful shutdown with connection draining\
' src/index.ts

# Create ARCHITECTURE.md
cat > ARCHITECTURE.md << 'ARCH'
# WebhookRelay Architecture

## Overview

WebhookRelay is a configurable webhook relay service that receives HTTP webhook
events and routes them through configurable delivery channels with filtering,
retry logic, and audit logging.

## System Architecture

```
                    ┌─────────────┐
                    │  HTTP POST  │
                    │  /webhooks  │
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │  Middleware  │  auth, logging, validation
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │ EventRouter │  orchestrates the pipeline
                    └──────┬──────┘
                           │
              ┌────────────┼────────────┐
              │            │            │
       ┌──────▼──────┐    │     ┌──────▼──────┐
       │   Filters   │    │     │  EventLog   │
       │ header/body │    │     │  (storage)  │
       └──────┬──────┘    │     └─────────────┘
              │            │
       ┌──────▼────────────▼────────────┐
       │      Channel Fan-Out           │
       │  (Promise.allSettled)          │
       ├──────────┬──────────┬──────────┤
       │   HTTP   │  Slack   │  Email   │
       │ Channel  │ Channel  │ Channel  │
       └──────────┴─────┬────┴──────────┘
                        │
                 ┌──────▼──────┐
                 │ RetryQueue  │  exponential backoff
                 └─────────────┘
```

## Key Design Decisions

### Decision: Generic Result Type Over Exceptions
- **Context**: Need consistent error handling across the pipeline
- **Choice**: Custom `Result<T, E>` type instead of try/catch
- **Reasoning**: Makes error paths explicit, composable, and testable
- **Trade-off**: More verbose than exceptions, but safer at boundaries

### Decision: Interface-Based Channel Abstraction
- **Context**: Multiple delivery transports with different protocols
- **Choice**: `Channel` interface with `send()` method per implementation
- **Reasoning**: Allows adding new channels without modifying router
- **Trade-off**: Each channel must handle its own serialization

### Decision: Composite Filter Pattern
- **Context**: Complex filtering rules combining multiple conditions
- **Choice**: Composite pattern with AND/OR/NONE modes
- **Reasoning**: Declarative filter trees instead of imperative chains
- **Trade-off**: Debugging deep composites can be harder

### Decision: Manual Dependency Injection
- **Context**: Wiring up components at startup
- **Choice**: Constructor-based DI in Application class, no framework
- **Reasoning**: Keeps dependencies explicit, avoids reflection/decorators overhead
- **Trade-off**: Wiring code grows with application size

### Decision: In-Memory Storage
- **Context**: Persistence requirements for MVP
- **Choice**: Map-based in-memory repositories with Repository<T> interface
- **Reasoning**: Quick to develop, easy to test, interface allows swap to DB later
- **Trade-off**: Data lost on restart, no durability guarantees

## Module Structure

| Module      | Purpose                          | Key Patterns              |
|-------------|----------------------------------|---------------------------|
| `types/`    | Core types, enums, Result<T>     | Discriminated unions, generics |
| `config/`   | Configuration loading/validation | Builder pattern, Partial<T> |
| `channels/` | Delivery channel implementations | Interface, Registry       |
| `filters/`  | Event filtering engine           | Composite, Strategy       |
| `retry/`    | Retry policies and queue         | Strategy, Generic Queue<T>|
| `router/`   | Event orchestration              | Mediator, Fan-out         |
| `storage/`  | Data persistence                 | Repository<T>, Generic    |
| `api/`      | Express routes and middleware    | Middleware chain           |

## Conventions

- **Barrel exports**: Every module directory has an `index.ts` re-exporting public API
- **Naming**: Interfaces are plain nouns (Channel, Filter), implementations are prefixed (HttpChannel, PayloadFilter)
- **Error handling**: Use `Result<T>` for business logic, exceptions for truly exceptional cases
- **Async**: All I/O operations are async/await, fan-out uses `Promise.allSettled`
- **Testing**: Vitest with mock injection, no integration tests yet
ARCH

commit "Add tech debt markers and ARCHITECTURE.md documentation" "2024-03-24T11:00:00"

# ============================================================
# Done — print summary
# ============================================================
echo ""
echo "TypeScript test repo created at $REPO_DIR"
echo ""
echo "Stats:"
echo "  Files: $(find . -type f -not -path './.git/*' | wc -l | tr -d ' ')"
echo "  LOC:   $(find . -type f -name '*.ts' -not -path './.git/*' | xargs wc -l 2>/dev/null | tail -1 | awk '{print $1}')"
echo "  Commits: $(git rev-list --count HEAD)"
echo ""
echo "To index with AtlasKB:"
echo "  go run ./cmd/atlaskb index --force $REPO_DIR"
