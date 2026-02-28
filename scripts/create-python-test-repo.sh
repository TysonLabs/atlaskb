#!/usr/bin/env bash
# create-python-test-repo.sh — Generates a Python test repo for AtlasKB pipeline benchmarking.
# Creates a "TaskFlow" FastAPI project (~1600 lines of Python) designed to stress cross-language support.
set -euo pipefail

REPO_DIR="/tmp/atlaskb-python-test-repo"

echo "Creating Python test repo at $REPO_DIR..."
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
# Commit 1: Initial project setup (build files)
# ============================================================
cat > pyproject.toml << 'PYPROJECT'
[build-system]
requires = ["setuptools>=68.0", "wheel"]
build-backend = "setuptools.backends._legacy:_Backend"

[project]
name = "taskflow"
version = "0.3.0"
description = "A task execution engine with pluggable executors and scheduling"
requires-python = ">=3.11"
dependencies = [
    "fastapi>=0.109.0",
    "uvicorn>=0.27.0",
    "pydantic>=2.5.0",
    "pydantic-settings>=2.1.0",
    "structlog>=24.1.0",
    "tenacity>=8.2.0",
    "asyncio-extras>=1.3.0",
]

[project.optional-dependencies]
dev = [
    "pytest>=8.0.0",
    "pytest-asyncio>=0.23.0",
    "httpx>=0.26.0",
    "coverage>=7.4.0",
    "mypy>=1.8.0",
    "ruff>=0.2.0",
]

[tool.ruff]
line-length = 100
target-version = "py311"

[tool.pytest.ini_options]
asyncio_mode = "auto"
testpaths = ["tests"]
PYPROJECT

cat > requirements.txt << 'REQS'
fastapi==0.109.2
uvicorn==0.27.1
pydantic==2.6.1
pydantic-settings==2.1.0
structlog==24.1.0
tenacity==8.2.3
pytest==8.0.1
pytest-asyncio==0.23.5
httpx==0.26.0
REQS

cat > README.md << 'README'
# TaskFlow

A task execution engine with pluggable executors and scheduling.

## Features
- Multiple executor backends (sync, async, retry-wrapped)
- Priority and round-robin scheduling
- FastAPI HTTP interface
- Structured logging
- Async context managers for task lifecycle
README

cat > Makefile << 'MAKEFILE'
.PHONY: install test lint fmt typecheck clean run

install:
	pip install -e ".[dev]"

test:
	pytest -v --tb=short

lint:
	ruff check .

fmt:
	ruff format .

typecheck:
	mypy taskflow/

clean:
	find . -type d -name __pycache__ -exec rm -rf {} +
	rm -rf .pytest_cache .mypy_cache dist *.egg-info

run:
	uvicorn taskflow.api.app:create_app --factory --reload

.DEFAULT_GOAL := test
MAKEFILE

mkdir -p taskflow taskflow/executor taskflow/engine taskflow/api taskflow/logging tests

commit "Initial project setup" "2024-06-01T10:00:00"

# ============================================================
# Commit 2: Core models and error types
# ============================================================
cat > taskflow/__init__.py << 'EOF'
"""TaskFlow — A task execution engine with pluggable executors and scheduling."""

from taskflow.models import Task, TaskStatus, TaskPriority, TaskResult
from taskflow.errors import TaskFlowError, ExecutionError, SchedulingError

__version__ = "0.3.0"
__all__ = [
    "Task",
    "TaskStatus",
    "TaskPriority",
    "TaskResult",
    "TaskFlowError",
    "ExecutionError",
    "SchedulingError",
    "__version__",
]
EOF

cat > taskflow/models.py << 'EOF'
"""Core domain models for the TaskFlow engine."""

from __future__ import annotations

import uuid
from dataclasses import dataclass, field
from datetime import datetime, timezone
from enum import Enum, IntEnum
from typing import Any

from pydantic import BaseModel, Field, field_validator


class TaskStatus(str, Enum):
    """Represents the lifecycle state of a task."""

    PENDING = "pending"
    RUNNING = "running"
    COMPLETED = "completed"
    FAILED = "failed"
    CANCELLED = "cancelled"
    RETRYING = "retrying"


class TaskPriority(IntEnum):
    """Priority levels for task scheduling. Lower value = higher priority."""

    CRITICAL = 0
    HIGH = 1
    NORMAL = 5
    LOW = 10
    BACKGROUND = 20


@dataclass
class Task:
    """Represents a unit of work to be executed.

    Tasks are the core domain object. They carry a payload, track their
    lifecycle status, and record execution metadata.
    """

    name: str
    payload: dict[str, Any] = field(default_factory=dict)
    priority: TaskPriority = TaskPriority.NORMAL
    status: TaskStatus = TaskStatus.PENDING
    id: str = field(default_factory=lambda: str(uuid.uuid4()))
    created_at: datetime = field(default_factory=lambda: datetime.now(timezone.utc))
    started_at: datetime | None = None
    completed_at: datetime | None = None
    error: str | None = None
    retries: int = 0
    max_retries: int = 3
    tags: list[str] = field(default_factory=list)

    @property
    def duration(self) -> float | None:
        """Return execution duration in seconds, or None if not completed."""
        if self.started_at and self.completed_at:
            return (self.completed_at - self.started_at).total_seconds()
        return None

    @property
    def is_terminal(self) -> bool:
        """Return True if the task is in a final state."""
        return self.status in (TaskStatus.COMPLETED, TaskStatus.FAILED, TaskStatus.CANCELLED)

    @property
    def can_retry(self) -> bool:
        """Return True if the task has remaining retry attempts."""
        return self.retries < self.max_retries and self.status == TaskStatus.FAILED

    def mark_running(self) -> None:
        """Transition task to running state."""
        self.status = TaskStatus.RUNNING
        self.started_at = datetime.now(timezone.utc)

    def mark_completed(self) -> None:
        """Transition task to completed state."""
        self.status = TaskStatus.COMPLETED
        self.completed_at = datetime.now(timezone.utc)
        self.error = None

    def mark_failed(self, error: str) -> None:
        """Transition task to failed state with error message."""
        self.status = TaskStatus.FAILED
        self.completed_at = datetime.now(timezone.utc)
        self.error = error

    def mark_cancelled(self) -> None:
        """Transition task to cancelled state."""
        self.status = TaskStatus.CANCELLED
        self.completed_at = datetime.now(timezone.utc)


class TaskResult(BaseModel):
    """Result of a task execution, returned by executors."""

    task_id: str
    success: bool
    output: Any = None
    error: str | None = None
    duration_ms: float = 0.0
    metadata: dict[str, Any] = Field(default_factory=dict)

    @field_validator("duration_ms")
    @classmethod
    def duration_must_be_non_negative(cls, v: float) -> float:
        if v < 0:
            raise ValueError("duration_ms must be non-negative")
        return v


class TaskSubmission(BaseModel):
    """API request model for submitting a new task."""

    name: str = Field(..., min_length=1, max_length=200)
    payload: dict[str, Any] = Field(default_factory=dict)
    priority: int = TaskPriority.NORMAL
    tags: list[str] = Field(default_factory=list)
    max_retries: int = Field(default=3, ge=0, le=10)


class TaskResponse(BaseModel):
    """API response model for task information."""

    id: str
    name: str
    status: TaskStatus
    priority: int
    created_at: datetime
    started_at: datetime | None = None
    completed_at: datetime | None = None
    error: str | None = None
    retries: int = 0
    tags: list[str] = Field(default_factory=list)
EOF

cat > taskflow/errors.py << 'EOF'
"""Exception hierarchy for the TaskFlow engine.

Provides a 3-deep exception hierarchy:
    TaskFlowError
    ├── ExecutionError
    │   ├── TimeoutError
    │   └── RetryExhaustedError
    ├── SchedulingError
    │   └── QueueFullError
    └── ValidationError
"""

from __future__ import annotations


class TaskFlowError(Exception):
    """Base exception for all TaskFlow errors."""

    def __init__(self, message: str, *, code: str = "TASKFLOW_ERROR") -> None:
        self.message = message
        self.code = code
        super().__init__(f"[{code}] {message}")


class ExecutionError(TaskFlowError):
    """Raised when task execution fails."""

    def __init__(self, message: str, *, task_id: str | None = None) -> None:
        self.task_id = task_id
        super().__init__(message, code="EXECUTION_ERROR")


class TimeoutError(ExecutionError):
    """Raised when task execution exceeds the timeout."""

    def __init__(self, task_id: str, timeout_seconds: float) -> None:
        self.timeout_seconds = timeout_seconds
        super().__init__(
            f"Task {task_id} timed out after {timeout_seconds}s",
            task_id=task_id,
        )


class RetryExhaustedError(ExecutionError):
    """Raised when all retry attempts have been exhausted."""

    def __init__(self, task_id: str, attempts: int, last_error: Exception) -> None:
        self.attempts = attempts
        self.last_error = last_error
        super().__init__(
            f"Task {task_id} failed after {attempts} attempts: {last_error}",
            task_id=task_id,
        )


class SchedulingError(TaskFlowError):
    """Raised when task scheduling fails."""

    def __init__(self, message: str) -> None:
        super().__init__(message, code="SCHEDULING_ERROR")


class QueueFullError(SchedulingError):
    """Raised when the task queue is at capacity."""

    def __init__(self, queue_size: int) -> None:
        self.queue_size = queue_size
        super().__init__(f"Task queue is full (capacity: {queue_size})")


class ValidationError(TaskFlowError):
    """Raised when input validation fails."""

    def __init__(self, field: str, message: str) -> None:
        self.field = field
        super().__init__(f"Validation failed for '{field}': {message}", code="VALIDATION_ERROR")
EOF

commit "Add core models and error types" "2024-06-05T10:00:00"

# ============================================================
# Commit 3: Configuration module
# ============================================================
cat > taskflow/config.py << 'EOF'
"""Application configuration loaded from environment variables."""

from __future__ import annotations

import os
from dataclasses import dataclass, field


@dataclass
class Config:
    """Application configuration with environment-based loading.

    All settings have sensible defaults and can be overridden via
    environment variables with the TASKFLOW_ prefix.
    """

    host: str = "0.0.0.0"
    port: int = 8000
    debug: bool = False
    max_workers: int = 4
    queue_size: int = 1000
    default_timeout: float = 30.0
    max_retries: int = 3
    log_level: str = "INFO"
    log_format: str = "json"
    cors_origins: list[str] = field(default_factory=lambda: ["*"])

    @classmethod
    def from_env(cls) -> Config:
        """Load configuration from environment variables.

        Environment variables are prefixed with TASKFLOW_ and uppercased.
        For example, TASKFLOW_PORT=9000 sets the port to 9000.
        """
        return cls(
            host=os.environ.get("TASKFLOW_HOST", "0.0.0.0"),
            port=int(os.environ.get("TASKFLOW_PORT", "8000")),
            debug=os.environ.get("TASKFLOW_DEBUG", "false").lower() == "true",
            max_workers=int(os.environ.get("TASKFLOW_MAX_WORKERS", "4")),
            queue_size=int(os.environ.get("TASKFLOW_QUEUE_SIZE", "1000")),
            default_timeout=float(os.environ.get("TASKFLOW_TIMEOUT", "30.0")),
            max_retries=int(os.environ.get("TASKFLOW_MAX_RETRIES", "3")),
            log_level=os.environ.get("TASKFLOW_LOG_LEVEL", "INFO"),
            log_format=os.environ.get("TASKFLOW_LOG_FORMAT", "json"),
        )

    def validate(self) -> None:
        """Validate configuration values.

        Raises ValueError if any setting is invalid.
        """
        if self.port < 1 or self.port > 65535:
            raise ValueError(f"Invalid port: {self.port}")
        if self.max_workers < 1:
            raise ValueError(f"max_workers must be positive, got {self.max_workers}")
        if self.queue_size < 1:
            raise ValueError(f"queue_size must be positive, got {self.queue_size}")
        if self.default_timeout <= 0:
            raise ValueError(f"timeout must be positive, got {self.default_timeout}")
        if self.log_level not in ("DEBUG", "INFO", "WARNING", "ERROR", "CRITICAL"):
            raise ValueError(f"Invalid log level: {self.log_level}")
EOF

commit "Add configuration module" "2024-06-08T10:00:00"

# ============================================================
# Commit 4: Executor interface + sync implementation
# ============================================================
cat > taskflow/executor/__init__.py << 'EOF'
"""Executor package — pluggable task execution backends."""

from taskflow.executor.base import Executor, ExecutorProtocol
from taskflow.executor.sync_executor import SyncExecutor, LoggingSyncExecutor

__all__ = ["Executor", "ExecutorProtocol", "SyncExecutor", "LoggingSyncExecutor"]
EOF

cat > taskflow/executor/base.py << 'EOF'
"""Base executor definitions using both Protocol and ABC.

This module demonstrates two common Python patterns for defining interfaces:
- Protocol: structural subtyping (duck typing with type checker support)
- ABC: nominal subtyping (explicit inheritance required)

Both are in the same file to stress the pipeline's ability to handle
multiple type definitions with similar method signatures.
"""

from __future__ import annotations

from abc import ABC, abstractmethod
from typing import Protocol, runtime_checkable

from taskflow.models import Task, TaskResult


@runtime_checkable
class ExecutorProtocol(Protocol):
    """Protocol for task executors — structural subtyping.

    Any class with a matching `execute` method satisfies this protocol
    without needing to inherit from it.
    """

    def execute(self, task: Task) -> TaskResult:
        """Execute a task and return the result."""
        ...

    @property
    def name(self) -> str:
        """Return the executor name."""
        ...


class Executor(ABC):
    """Abstract base class for task executors — nominal subtyping.

    Subclasses must implement `execute` and `name`. This provides
    a common base for shared executor behavior.
    """

    @abstractmethod
    def execute(self, task: Task) -> TaskResult:
        """Execute a task and return the result."""
        ...

    @property
    @abstractmethod
    def name(self) -> str:
        """Return the executor name."""
        ...

    def validate_task(self, task: Task) -> None:
        """Validate that a task can be executed.

        Raises ExecutionError if the task is invalid.
        """
        from taskflow.errors import ExecutionError

        if task.is_terminal:
            raise ExecutionError(
                f"Cannot execute task in terminal state: {task.status}",
                task_id=task.id,
            )
        if not task.name:
            raise ExecutionError("Task name must not be empty", task_id=task.id)
EOF

cat > taskflow/executor/sync_executor.py << 'EOF'
"""Synchronous task executor with optional logging mixin.

Demonstrates:
- Concrete executor implementation
- Mixin pattern with diamond MRO
- Same-name `execute` method (dedup stress with base.py)
"""

from __future__ import annotations

import time
from typing import Any

from taskflow.errors import ExecutionError
from taskflow.executor.base import Executor
from taskflow.models import Task, TaskResult


class SyncExecutor(Executor):
    """Executes tasks synchronously in the calling thread.

    This is the simplest executor — it runs the task payload handler
    directly and blocks until completion.
    """

    def __init__(self, handlers: dict[str, Any] | None = None) -> None:
        self._handlers: dict[str, Any] = handlers or {}

    @property
    def name(self) -> str:
        return "sync"

    def execute(self, task: Task) -> TaskResult:
        """Execute a task synchronously.

        Looks up a handler by task name and calls it with the task payload.
        Falls back to a no-op handler if no matching handler is found.
        """
        self.validate_task(task)
        task.mark_running()
        start = time.monotonic()

        try:
            handler = self._handlers.get(task.name, self._default_handler)
            output = handler(task.payload)
            elapsed = (time.monotonic() - start) * 1000
            task.mark_completed()
            return TaskResult(
                task_id=task.id,
                success=True,
                output=output,
                duration_ms=elapsed,
            )
        except Exception as exc:
            elapsed = (time.monotonic() - start) * 1000
            task.mark_failed(str(exc))
            return TaskResult(
                task_id=task.id,
                success=False,
                error=str(exc),
                duration_ms=elapsed,
            )

    def register_handler(self, task_name: str, handler: Any) -> None:
        """Register a handler function for a specific task name."""
        self._handlers[task_name] = handler

    @staticmethod
    def _default_handler(payload: dict[str, Any]) -> str:
        """Default no-op handler for unregistered task names."""
        return f"processed {len(payload)} fields"


class LoggingMixin:
    """Mixin that adds logging to executor methods.

    When combined with an Executor subclass, logs before and after
    each task execution.
    """

    def log(self, message: str) -> None:
        """Log a message. Override to customize logging destination."""
        print(f"[{self.__class__.__name__}] {message}")


class LoggingSyncExecutor(LoggingMixin, SyncExecutor):
    """SyncExecutor with logging via mixin (diamond MRO).

    MRO: LoggingSyncExecutor -> LoggingMixin -> SyncExecutor -> Executor -> ABC
    This exercises the pipeline's ability to resolve diamond inheritance.
    """

    @property
    def name(self) -> str:
        return "sync-logging"

    def execute(self, task: Task) -> TaskResult:
        """Execute with pre/post logging."""
        self.log(f"Starting task: {task.name} (id={task.id})")
        result = super().execute(task)
        if result.success:
            self.log(f"Task completed: {task.name} in {result.duration_ms:.1f}ms")
        else:
            self.log(f"Task failed: {task.name} — {result.error}")
        return result
EOF

commit "Add executor interface and sync implementation" "2024-06-12T10:00:00"

# ============================================================
# Commit 5: Async executor with batch support
# ============================================================
cat > taskflow/executor/async_executor.py << 'EOF'
"""Asynchronous task executor with batch processing support.

Demonstrates:
- Async execute method (same name as sync executors — dedup stress)
- asyncio.gather for concurrent batch execution
- Async context manager usage
"""

from __future__ import annotations

import asyncio
import time
from typing import Any, Callable, Coroutine

from taskflow.errors import ExecutionError, TimeoutError
from taskflow.models import Task, TaskResult


class AsyncExecutor:
    """Executes tasks asynchronously using asyncio.

    Unlike SyncExecutor, this executor runs handlers as coroutines
    and supports concurrent batch execution via asyncio.gather.
    """

    def __init__(
        self,
        handlers: dict[str, Callable[..., Coroutine]] | None = None,
        timeout: float = 30.0,
    ) -> None:
        self._handlers: dict[str, Callable[..., Coroutine]] = handlers or {}
        self._timeout = timeout
        self._running: set[str] = set()

    @property
    def name(self) -> str:
        return "async"

    @property
    def running_count(self) -> int:
        """Return the number of currently executing tasks."""
        return len(self._running)

    async def execute(self, task: Task) -> TaskResult:
        """Execute a task asynchronously with timeout.

        The handler coroutine is wrapped in asyncio.wait_for to enforce
        the configured timeout.
        """
        if task.is_terminal:
            raise ExecutionError(
                f"Cannot execute task in terminal state: {task.status}",
                task_id=task.id,
            )

        task.mark_running()
        self._running.add(task.id)
        start = time.monotonic()

        try:
            handler = self._handlers.get(task.name, self._default_handler)
            output = await asyncio.wait_for(handler(task.payload), timeout=self._timeout)
            elapsed = (time.monotonic() - start) * 1000
            task.mark_completed()
            return TaskResult(
                task_id=task.id,
                success=True,
                output=output,
                duration_ms=elapsed,
            )
        except asyncio.TimeoutError:
            elapsed = (time.monotonic() - start) * 1000
            task.mark_failed(f"Timed out after {self._timeout}s")
            raise TimeoutError(task.id, self._timeout)
        except Exception as exc:
            elapsed = (time.monotonic() - start) * 1000
            task.mark_failed(str(exc))
            return TaskResult(
                task_id=task.id,
                success=False,
                error=str(exc),
                duration_ms=elapsed,
            )
        finally:
            self._running.discard(task.id)

    async def execute_batch(self, tasks: list[Task]) -> list[TaskResult]:
        """Execute multiple tasks concurrently using asyncio.gather.

        Failed tasks do not cancel other tasks in the batch.
        """
        coros = [self.execute(task) for task in tasks]
        results = await asyncio.gather(*coros, return_exceptions=True)

        final: list[TaskResult] = []
        for task, result in zip(tasks, results):
            if isinstance(result, Exception):
                final.append(
                    TaskResult(
                        task_id=task.id,
                        success=False,
                        error=str(result),
                    )
                )
            else:
                final.append(result)
        return final

    def register_handler(self, task_name: str, handler: Callable[..., Coroutine]) -> None:
        """Register an async handler for a specific task name."""
        self._handlers[task_name] = handler

    @staticmethod
    async def _default_handler(payload: dict[str, Any]) -> str:
        """Default async no-op handler."""
        await asyncio.sleep(0)
        return f"async processed {len(payload)} fields"
EOF

commit "Add async executor with batch support" "2024-06-15T10:00:00"

# ============================================================
# Commit 6: Retry decorator and retry executor
# ============================================================
cat > taskflow/executor/retry.py << 'EOF'
"""Retry logic for task execution — decorator factory and executor wrapper.

Demonstrates:
- Decorator factory pattern (with_retry)
- Executor wrapper (RetryExecutor) with same-name execute method
- Composition over inheritance
"""

from __future__ import annotations

import functools
import time
from typing import Any, Callable, TypeVar

from taskflow.errors import RetryExhaustedError
from taskflow.executor.base import Executor
from taskflow.models import Task, TaskResult

F = TypeVar("F", bound=Callable[..., Any])


def with_retry(
    max_attempts: int = 3,
    base_delay: float = 0.1,
    max_delay: float = 5.0,
    backoff_factor: float = 2.0,
) -> Callable[[F], F]:
    """Decorator factory for adding retry logic to any callable.

    Usage:
        @with_retry(max_attempts=5, base_delay=0.5)
        def flaky_operation():
            ...

    The decorator applies exponential backoff between retries.
    """

    def decorator(func: F) -> F:
        @functools.wraps(func)
        def wrapper(*args: Any, **kwargs: Any) -> Any:
            last_error: Exception | None = None
            delay = base_delay

            for attempt in range(1, max_attempts + 1):
                try:
                    return func(*args, **kwargs)
                except Exception as exc:
                    last_error = exc
                    if attempt < max_attempts:
                        time.sleep(min(delay, max_delay))
                        delay *= backoff_factor

            raise last_error  # type: ignore[misc]

        return wrapper  # type: ignore[return-value]

    return decorator


class RetryExecutor(Executor):
    """Wraps another Executor and retries failed executions.

    This uses composition: it delegates to an inner executor and
    handles retry logic externally. The execute method has the same
    signature as all other executors.
    """

    def __init__(
        self,
        inner: Executor,
        max_retries: int = 3,
        base_delay: float = 0.1,
    ) -> None:
        self._inner = inner
        self._max_retries = max_retries
        self._base_delay = base_delay

    @property
    def name(self) -> str:
        return f"retry({self._inner.name})"

    def execute(self, task: Task) -> TaskResult:
        """Execute with retries, delegating to the inner executor.

        On failure, the task's retry counter is incremented and the
        inner executor is called again with exponential backoff.
        """
        last_result: TaskResult | None = None
        delay = self._base_delay

        for attempt in range(1, self._max_retries + 1):
            # Reset task state for retry
            if attempt > 1:
                task.status = task.status.PENDING
                task.retries = attempt - 1
                time.sleep(delay)
                delay *= 2

            last_result = self._inner.execute(task)
            if last_result.success:
                return last_result

        # All retries exhausted
        if last_result and last_result.error:
            raise RetryExhaustedError(
                task.id,
                self._max_retries,
                Exception(last_result.error),
            )

        return last_result  # type: ignore[return-value]
EOF

commit "Add retry decorator and retry executor" "2024-06-18T10:00:00"

# ============================================================
# Commit 7: Engine with orchestrator and scheduler
# ============================================================
cat > taskflow/engine/__init__.py << 'EOF'
"""Engine package — task orchestration and scheduling."""

from taskflow.engine.orchestrator import Orchestrator
from taskflow.engine.scheduler import PriorityScheduler, RoundRobinScheduler

__all__ = ["Orchestrator", "PriorityScheduler", "RoundRobinScheduler"]
EOF

cat > taskflow/engine/orchestrator.py << 'EOF'
"""Main orchestrator that coordinates task submission, scheduling, and execution.

This is the central component of the TaskFlow engine. It connects
schedulers to executors and manages the task lifecycle.
"""

from __future__ import annotations

import asyncio
import time
from collections import defaultdict
from typing import Any

from taskflow.errors import SchedulingError, QueueFullError
from taskflow.executor.base import Executor
from taskflow.models import Task, TaskPriority, TaskResult, TaskStatus


class Orchestrator:
    """Coordinates task scheduling and execution.

    The orchestrator maintains a task registry, delegates scheduling to
    a pluggable scheduler, and dispatches tasks to executors.
    """

    def __init__(
        self,
        executor: Executor,
        max_queue_size: int = 1000,
        max_concurrent: int = 10,
    ) -> None:
        self._executor = executor
        self._max_queue_size = max_queue_size
        self._max_concurrent = max_concurrent
        self._tasks: dict[str, Task] = {}
        self._results: dict[str, TaskResult] = {}
        self._queue: list[Task] = []
        self._stats: dict[str, int] = defaultdict(int)
        self._running = False

    @property
    def is_running(self) -> bool:
        """Return True if the orchestrator is accepting tasks."""
        return self._running

    @property
    def pending_count(self) -> int:
        """Return the number of tasks waiting in the queue."""
        return len(self._queue)

    def start(self) -> None:
        """Start the orchestrator and begin accepting tasks."""
        self._running = True
        self._stats["started_at"] = int(time.time())

    def stop(self) -> None:
        """Stop the orchestrator gracefully."""
        self._running = False

    def submit(self, task: Task) -> str:
        """Submit a task for execution.

        Returns the task ID. Raises QueueFullError if the queue is at capacity.
        Raises SchedulingError if the orchestrator is not running.
        """
        if not self._running:
            raise SchedulingError("Orchestrator is not running")
        if len(self._queue) >= self._max_queue_size:
            raise QueueFullError(self._max_queue_size)

        self._tasks[task.id] = task
        self._queue.append(task)
        self._stats["submitted"] += 1
        return task.id

    def run(self, task: Task) -> TaskResult:
        """Submit and immediately execute a task synchronously.

        This bypasses the queue and executes directly.
        """
        self._tasks[task.id] = task
        self._stats["submitted"] += 1

        result = self._executor.execute(task)
        self._results[task.id] = result

        if result.success:
            self._stats["completed"] += 1
        else:
            self._stats["failed"] += 1

        return result

    async def run_async(self, task: Task) -> TaskResult:
        """Execute a task asynchronously if the executor supports it."""
        self._tasks[task.id] = task
        self._stats["submitted"] += 1

        if hasattr(self._executor, "execute") and asyncio.iscoroutinefunction(
            self._executor.execute
        ):
            result = await self._executor.execute(task)
        else:
            result = self._executor.execute(task)

        self._results[task.id] = result
        if result.success:
            self._stats["completed"] += 1
        else:
            self._stats["failed"] += 1
        return result

    def cancel(self, task_id: str) -> bool:
        """Cancel a pending task. Returns True if the task was cancelled."""
        task = self._tasks.get(task_id)
        if task is None:
            return False
        if task.is_terminal:
            return False

        task.mark_cancelled()
        self._queue = [t for t in self._queue if t.id != task_id]
        self._stats["cancelled"] += 1
        return True

    def get_task(self, task_id: str) -> Task | None:
        """Look up a task by ID."""
        return self._tasks.get(task_id)

    def get_result(self, task_id: str) -> TaskResult | None:
        """Look up a task result by ID."""
        return self._results.get(task_id)

    def metrics(self) -> dict[str, Any]:
        """Return orchestrator metrics."""
        return {
            "is_running": self._running,
            "total_submitted": self._stats["submitted"],
            "total_completed": self._stats["completed"],
            "total_failed": self._stats["failed"],
            "total_cancelled": self._stats["cancelled"],
            "queue_size": len(self._queue),
            "executor": self._executor.name,
        }

    def drain_queue(self) -> list[TaskResult]:
        """Execute all queued tasks and return results."""
        results: list[TaskResult] = []
        while self._queue:
            task = self._queue.pop(0)
            result = self._executor.execute(task)
            self._results[task.id] = result
            results.append(result)
            if result.success:
                self._stats["completed"] += 1
            else:
                self._stats["failed"] += 1
        return results
EOF

cat > taskflow/engine/scheduler.py << 'EOF'
"""Task schedulers for controlling execution order.

Provides two scheduling strategies:
- PriorityScheduler: tasks ordered by priority (lower value = higher priority)
- RoundRobinScheduler: tasks distributed across named queues

Both implement the same interface (duck typing / structural subtyping).
"""

from __future__ import annotations

import heapq
from collections import deque
from typing import Sequence

from taskflow.models import Task


class PriorityScheduler:
    """Schedules tasks by priority using a min-heap.

    Lower priority values are executed first (CRITICAL=0 before LOW=10).
    Tasks with equal priority are executed in FIFO order.
    """

    def __init__(self) -> None:
        self._heap: list[tuple[int, int, Task]] = []
        self._counter = 0

    @property
    def name(self) -> str:
        return "priority"

    @property
    def size(self) -> int:
        return len(self._heap)

    def schedule(self, task: Task) -> None:
        """Add a task to the priority queue."""
        heapq.heappush(self._heap, (task.priority, self._counter, task))
        self._counter += 1

    def next(self) -> Task | None:
        """Pop the highest-priority task, or None if empty."""
        if not self._heap:
            return None
        _, _, task = heapq.heappop(self._heap)
        return task

    def peek(self) -> Task | None:
        """View the highest-priority task without removing it."""
        if not self._heap:
            return None
        return self._heap[0][2]

    def schedule_batch(self, tasks: Sequence[Task]) -> int:
        """Schedule multiple tasks at once. Returns count scheduled."""
        for task in tasks:
            self.schedule(task)
        return len(tasks)


class RoundRobinScheduler:
    """Distributes tasks across named queues in round-robin order.

    Each queue is identified by a string key (e.g., executor name or
    task category). Tasks are dequeued one per queue in rotation.
    """

    def __init__(self, queue_names: list[str] | None = None) -> None:
        self._queues: dict[str, deque[Task]] = {}
        self._queue_order: list[str] = []
        self._current_index = 0
        if queue_names:
            for name in queue_names:
                self._queues[name] = deque()
                self._queue_order.append(name)

    @property
    def name(self) -> str:
        return "round-robin"

    @property
    def size(self) -> int:
        return sum(len(q) for q in self._queues.values())

    def schedule(self, task: Task, queue_name: str | None = None) -> None:
        """Add a task to a specific queue, or the first queue if not specified."""
        target = queue_name or (self._queue_order[0] if self._queue_order else "default")
        if target not in self._queues:
            self._queues[target] = deque()
            self._queue_order.append(target)
        self._queues[target].append(task)

    def next(self) -> Task | None:
        """Get the next task using round-robin across queues."""
        if not self._queue_order:
            return None

        # Try each queue starting from current index
        for _ in range(len(self._queue_order)):
            queue_name = self._queue_order[self._current_index]
            self._current_index = (self._current_index + 1) % len(self._queue_order)
            queue = self._queues.get(queue_name)
            if queue:
                return queue.popleft()
        return None

    def schedule_batch(self, tasks: Sequence[Task], queue_name: str | None = None) -> int:
        """Schedule multiple tasks into the same queue."""
        for task in tasks:
            self.schedule(task, queue_name)
        return len(tasks)
EOF

commit "Add engine with orchestrator and scheduler" "2024-06-22T10:00:00"

# ============================================================
# Commit 8: Task execution context managers
# ============================================================
cat > taskflow/engine/context.py << 'EOF'
"""Context managers for task execution lifecycle.

Provides both synchronous and asynchronous context managers that
handle task state transitions (running → completed/failed) and
resource cleanup.
"""

from __future__ import annotations

import time
from types import TracebackType
from typing import Any

from taskflow.models import Task, TaskResult


class TaskContext:
    """Synchronous context manager for task execution.

    Manages the task lifecycle: marks the task as running on entry,
    and completed/failed on exit depending on whether an exception
    was raised.

    Usage:
        with TaskContext(task) as ctx:
            result = do_work(task.payload)
            ctx.set_output(result)
    """

    def __init__(self, task: Task) -> None:
        self._task = task
        self._output: Any = None
        self._start_time: float = 0

    def __enter__(self) -> TaskContext:
        self._task.mark_running()
        self._start_time = time.monotonic()
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_val: BaseException | None,
        exc_tb: TracebackType | None,
    ) -> bool:
        elapsed = (time.monotonic() - self._start_time) * 1000
        if exc_type is not None:
            self._task.mark_failed(str(exc_val))
            return False  # re-raise the exception
        self._task.mark_completed()
        return False

    def set_output(self, output: Any) -> None:
        """Store the task output for later retrieval."""
        self._output = output

    @property
    def result(self) -> TaskResult:
        """Build a TaskResult from the current state."""
        elapsed = (time.monotonic() - self._start_time) * 1000 if self._start_time else 0
        return TaskResult(
            task_id=self._task.id,
            success=self._task.status.value == "completed",
            output=self._output,
            error=self._task.error,
            duration_ms=elapsed,
        )


class AsyncTaskContext:
    """Asynchronous context manager for task execution.

    Same lifecycle management as TaskContext, but supports async with.
    """

    def __init__(self, task: Task) -> None:
        self._task = task
        self._output: Any = None
        self._start_time: float = 0

    async def __aenter__(self) -> AsyncTaskContext:
        self._task.mark_running()
        self._start_time = time.monotonic()
        return self

    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc_val: BaseException | None,
        exc_tb: TracebackType | None,
    ) -> bool:
        if exc_type is not None:
            self._task.mark_failed(str(exc_val))
            return False
        self._task.mark_completed()
        return False

    def set_output(self, output: Any) -> None:
        """Store the task output."""
        self._output = output

    @property
    def result(self) -> TaskResult:
        """Build a TaskResult from the current state."""
        elapsed = (time.monotonic() - self._start_time) * 1000 if self._start_time else 0
        return TaskResult(
            task_id=self._task.id,
            success=self._task.status.value == "completed",
            output=self._output,
            error=self._task.error,
            duration_ms=elapsed,
        )
EOF

commit "Add task execution context managers" "2024-06-25T10:00:00"

# ============================================================
# Commit 9: FastAPI app with routes and middleware
# ============================================================
cat > taskflow/api/__init__.py << 'EOF'
"""API package — FastAPI HTTP interface for TaskFlow."""

from taskflow.api.app import create_app

__all__ = ["create_app"]
EOF

cat > taskflow/api/app.py << 'EOF'
"""FastAPI application factory with async lifespan management."""

from __future__ import annotations

from contextlib import asynccontextmanager
from typing import AsyncIterator

from fastapi import FastAPI

from taskflow.api.middleware import RequestLoggingMiddleware, RateLimitMiddleware
from taskflow.api.routes import router
from taskflow.config import Config


@asynccontextmanager
async def lifespan(app: FastAPI) -> AsyncIterator[None]:
    """Manage application startup and shutdown."""
    config = Config.from_env()
    app.state.config = config
    # Startup: initialize resources
    yield
    # Shutdown: cleanup resources


def create_app(config: Config | None = None) -> FastAPI:
    """Application factory — creates and configures the FastAPI app.

    Uses the factory pattern for testability and configuration flexibility.
    """
    app = FastAPI(
        title="TaskFlow",
        version="0.3.0",
        description="Task execution engine API",
        lifespan=lifespan,
    )

    # Add middleware (order matters — last added is outermost)
    app.add_middleware(RateLimitMiddleware, max_requests=100, window_seconds=60)
    app.add_middleware(RequestLoggingMiddleware)

    # Include routes
    app.include_router(router)

    return app
EOF

cat > taskflow/api/routes.py << 'EOF'
"""API route definitions for the TaskFlow engine.

Six endpoints in one file to stress the pipeline's endpoint extraction.
"""

from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException, status

from taskflow.api.dependencies import get_orchestrator, get_config
from taskflow.engine.orchestrator import Orchestrator
from taskflow.config import Config
from taskflow.models import (
    Task,
    TaskPriority,
    TaskResponse,
    TaskResult,
    TaskSubmission,
)

router = APIRouter(prefix="/api/v1", tags=["tasks"])


@router.post("/tasks", response_model=TaskResponse, status_code=status.HTTP_201_CREATED)
async def submit_task(
    submission: TaskSubmission,
    orchestrator: Orchestrator = Depends(get_orchestrator),
) -> TaskResponse:
    """Submit a new task for execution."""
    task = Task(
        name=submission.name,
        payload=submission.payload,
        priority=TaskPriority(submission.priority),
        tags=submission.tags,
        max_retries=submission.max_retries,
    )
    orchestrator.submit(task)
    return _task_to_response(task)


@router.get("/tasks/{task_id}", response_model=TaskResponse)
async def get_task(
    task_id: str,
    orchestrator: Orchestrator = Depends(get_orchestrator),
) -> TaskResponse:
    """Get task details by ID."""
    task = orchestrator.get_task(task_id)
    if task is None:
        raise HTTPException(status_code=404, detail=f"Task {task_id} not found")
    return _task_to_response(task)


@router.post("/tasks/{task_id}/cancel", response_model=dict)
async def cancel_task(
    task_id: str,
    orchestrator: Orchestrator = Depends(get_orchestrator),
) -> dict:
    """Cancel a pending task."""
    cancelled = orchestrator.cancel(task_id)
    if not cancelled:
        raise HTTPException(status_code=400, detail=f"Task {task_id} cannot be cancelled")
    return {"status": "cancelled", "task_id": task_id}


@router.get("/tasks/{task_id}/result", response_model=TaskResult)
async def get_task_result(
    task_id: str,
    orchestrator: Orchestrator = Depends(get_orchestrator),
) -> TaskResult:
    """Get the execution result for a completed task."""
    result = orchestrator.get_result(task_id)
    if result is None:
        raise HTTPException(status_code=404, detail=f"No result for task {task_id}")
    return result


@router.get("/metrics", response_model=dict)
async def get_metrics(
    orchestrator: Orchestrator = Depends(get_orchestrator),
) -> dict:
    """Get orchestrator runtime metrics."""
    return orchestrator.metrics()


@router.get("/health", response_model=dict)
async def health_check(
    config: Config = Depends(get_config),
) -> dict:
    """Application health check endpoint."""
    return {
        "status": "healthy",
        "version": "0.3.0",
        "debug": config.debug,
    }


def _task_to_response(task: Task) -> TaskResponse:
    """Convert a domain Task to an API response model."""
    return TaskResponse(
        id=task.id,
        name=task.name,
        status=task.status,
        priority=task.priority,
        created_at=task.created_at,
        started_at=task.started_at,
        completed_at=task.completed_at,
        error=task.error,
        retries=task.retries,
        tags=task.tags,
    )
EOF

cat > taskflow/api/middleware.py << 'EOF'
"""ASGI middleware for request logging and rate limiting.

Both middleware classes have a `dispatch` method — same-name method
stress for the pipeline's dedup logic.
"""

from __future__ import annotations

import time
from collections import defaultdict
from typing import Callable

from starlette.middleware.base import BaseHTTPMiddleware
from starlette.requests import Request
from starlette.responses import JSONResponse, Response


class RequestLoggingMiddleware(BaseHTTPMiddleware):
    """Logs request method, path, status code, and duration.

    Attaches timing information to each request for observability.
    """

    async def dispatch(self, request: Request, call_next: Callable) -> Response:
        """Process request with logging."""
        start = time.monotonic()
        response = await call_next(request)
        duration = (time.monotonic() - start) * 1000

        print(
            f"{request.method} {request.url.path} "
            f"status={response.status_code} "
            f"duration={duration:.1f}ms"
        )
        response.headers["X-Request-Duration-Ms"] = f"{duration:.1f}"
        return response


class RateLimitMiddleware(BaseHTTPMiddleware):
    """Simple in-memory rate limiter by client IP.

    Tracks request counts per IP within a sliding time window.
    Returns 429 Too Many Requests when the limit is exceeded.
    """

    def __init__(self, app, max_requests: int = 100, window_seconds: int = 60) -> None:
        super().__init__(app)
        self._max_requests = max_requests
        self._window = window_seconds
        self._requests: dict[str, list[float]] = defaultdict(list)

    async def dispatch(self, request: Request, call_next: Callable) -> Response:
        """Process request with rate limiting."""
        client_ip = request.client.host if request.client else "unknown"
        now = time.monotonic()

        # Clean old entries
        self._requests[client_ip] = [
            t for t in self._requests[client_ip] if now - t < self._window
        ]

        if len(self._requests[client_ip]) >= self._max_requests:
            return JSONResponse(
                status_code=429,
                content={"detail": "Rate limit exceeded"},
            )

        self._requests[client_ip].append(now)
        return await call_next(request)
EOF

cat > taskflow/api/dependencies.py << 'EOF'
"""FastAPI dependency injection functions.

These functions are used with FastAPI's Depends() to inject
shared resources into route handlers.
"""

from __future__ import annotations

from typing import Generator

from taskflow.config import Config
from taskflow.engine.orchestrator import Orchestrator
from taskflow.executor.sync_executor import SyncExecutor

# Module-level singletons (initialized on first access)
_orchestrator: Orchestrator | None = None
_config: Config | None = None


def get_config() -> Config:
    """Provide the application configuration."""
    global _config
    if _config is None:
        _config = Config.from_env()
    return _config


def get_orchestrator() -> Orchestrator:
    """Provide the task orchestrator singleton.

    Creates a default orchestrator with a SyncExecutor if one
    hasn't been configured yet.
    """
    global _orchestrator
    if _orchestrator is None:
        executor = SyncExecutor()
        config = get_config()
        _orchestrator = Orchestrator(
            executor=executor,
            max_queue_size=config.queue_size,
            max_concurrent=config.max_workers,
        )
        _orchestrator.start()
    return _orchestrator


def reset_dependencies() -> None:
    """Reset singletons (for testing)."""
    global _orchestrator, _config
    _orchestrator = None
    _config = None
EOF

commit "Add FastAPI app with routes and middleware" "2024-06-28T10:00:00"

# ============================================================
# Commit 10: Structured logging package
# ============================================================
cat > taskflow/logging/__init__.py << 'EOF'
"""Logging package — structured logging for TaskFlow."""

from taskflow.logging.structured import StructuredLogger, NullLogger

__all__ = ["StructuredLogger", "NullLogger"]
EOF

cat > taskflow/logging/structured.py << 'EOF'
"""Structured logger using structlog-style key-value pairs.

Provides StructuredLogger (production) and NullLogger (testing).
Both have the same method signatures — same-name method stress.
"""

from __future__ import annotations

import json
import sys
import time
from typing import Any, TextIO


class StructuredLogger:
    """JSON structured logger for production use.

    Outputs one JSON object per log line with timestamp, level,
    message, and arbitrary key-value context.
    """

    def __init__(
        self,
        name: str = "taskflow",
        level: str = "INFO",
        output: TextIO = sys.stderr,
    ) -> None:
        self._name = name
        self._level = level
        self._output = output
        self._context: dict[str, Any] = {}
        self._levels = {"DEBUG": 10, "INFO": 20, "WARNING": 30, "ERROR": 40, "CRITICAL": 50}

    def bind(self, **kwargs: Any) -> StructuredLogger:
        """Return a new logger with additional context keys."""
        new = StructuredLogger(self._name, self._level, self._output)
        new._context = {**self._context, **kwargs}
        return new

    def debug(self, msg: str, **kwargs: Any) -> None:
        """Log at DEBUG level."""
        self._log("DEBUG", msg, kwargs)

    def info(self, msg: str, **kwargs: Any) -> None:
        """Log at INFO level."""
        self._log("INFO", msg, kwargs)

    def warning(self, msg: str, **kwargs: Any) -> None:
        """Log at WARNING level."""
        self._log("WARNING", msg, kwargs)

    def error(self, msg: str, **kwargs: Any) -> None:
        """Log at ERROR level."""
        self._log("ERROR", msg, kwargs)

    def _log(self, level: str, msg: str, extra: dict[str, Any]) -> None:
        """Internal log method — writes a JSON line."""
        if self._levels.get(level, 0) < self._levels.get(self._level, 0):
            return
        entry = {
            "timestamp": time.time(),
            "level": level,
            "logger": self._name,
            "message": msg,
            **self._context,
            **extra,
        }
        self._output.write(json.dumps(entry) + "\n")
        self._output.flush()


class NullLogger:
    """Logger that discards all output. Used in tests."""

    def bind(self, **kwargs: Any) -> NullLogger:
        return self

    def debug(self, msg: str, **kwargs: Any) -> None:
        pass

    def info(self, msg: str, **kwargs: Any) -> None:
        pass

    def warning(self, msg: str, **kwargs: Any) -> None:
        pass

    def error(self, msg: str, **kwargs: Any) -> None:
        pass
EOF

cat > taskflow/logging/handlers.py << 'EOF'
"""Custom log handlers and formatters.

Provides JSONFormatter for stdlib logging integration and
AsyncLogHandler for non-blocking log writes.
"""

from __future__ import annotations

import asyncio
import json
import logging
import time
from typing import Any


class JSONFormatter(logging.Formatter):
    """Formats log records as single-line JSON objects.

    Compatible with structured log aggregators like ELK, Datadog, etc.
    """

    def __init__(self, extra_fields: dict[str, Any] | None = None) -> None:
        super().__init__()
        self._extra = extra_fields or {}

    def format(self, record: logging.LogRecord) -> str:
        """Format a log record as JSON."""
        entry = {
            "timestamp": time.time(),
            "level": record.levelname,
            "logger": record.name,
            "message": record.getMessage(),
            "module": record.module,
            "line": record.lineno,
            **self._extra,
        }
        if record.exc_info and record.exc_info[1]:
            entry["exception"] = str(record.exc_info[1])
        return json.dumps(entry)


class AsyncLogHandler(logging.Handler):
    """Non-blocking log handler that writes via asyncio.

    Queues log entries and writes them asynchronously to avoid
    blocking the event loop during high-throughput logging.
    """

    def __init__(self, capacity: int = 1000) -> None:
        super().__init__()
        self._queue: asyncio.Queue[str] | None = None
        self._capacity = capacity
        self._dropped = 0

    def emit(self, record: logging.LogRecord) -> None:
        """Queue a log record for async writing."""
        try:
            msg = self.format(record)
            if self._queue and not self._queue.full():
                self._queue.put_nowait(msg)
            else:
                self._dropped += 1
        except Exception:
            self.handleError(record)

    async def start(self) -> None:
        """Initialize the async queue."""
        self._queue = asyncio.Queue(maxsize=self._capacity)

    async def drain(self) -> list[str]:
        """Drain all queued log messages."""
        messages: list[str] = []
        if self._queue:
            while not self._queue.empty():
                messages.append(await self._queue.get())
        return messages

    @property
    def dropped_count(self) -> int:
        """Return the number of dropped log messages."""
        return self._dropped
EOF

commit "Add structured logging package" "2024-07-01T10:00:00"

# ============================================================
# Commit 11: Test suite
# ============================================================
cat > tests/__init__.py << 'EOF'
"""TaskFlow test suite."""
EOF

cat > tests/conftest.py << 'EOF'
"""Shared test fixtures for the TaskFlow test suite."""

from __future__ import annotations

import pytest

from taskflow.config import Config
from taskflow.engine.orchestrator import Orchestrator
from taskflow.executor.sync_executor import SyncExecutor
from taskflow.models import Task, TaskPriority


@pytest.fixture
def config() -> Config:
    """Provide a test configuration."""
    return Config(debug=True, max_workers=2, queue_size=100)


@pytest.fixture
def executor() -> SyncExecutor:
    """Provide a sync executor with a test handler."""
    ex = SyncExecutor()
    ex.register_handler("echo", lambda p: p.get("message", ""))
    ex.register_handler("fail", lambda p: (_ for _ in ()).throw(ValueError("intentional")))
    return ex


@pytest.fixture
def orchestrator(executor: SyncExecutor) -> Orchestrator:
    """Provide a running orchestrator with a sync executor."""
    orch = Orchestrator(executor=executor, max_queue_size=100)
    orch.start()
    yield orch
    orch.stop()


@pytest.fixture
def sample_task() -> Task:
    """Provide a sample task for testing."""
    return Task(
        name="echo",
        payload={"message": "hello"},
        priority=TaskPriority.NORMAL,
    )


@pytest.fixture
def high_priority_task() -> Task:
    """Provide a high-priority task."""
    return Task(
        name="echo",
        payload={"message": "urgent"},
        priority=TaskPriority.HIGH,
    )
EOF

cat > tests/test_models.py << 'EOF'
"""Tests for core models."""

from __future__ import annotations

import pytest

from taskflow.models import (
    Task,
    TaskPriority,
    TaskResult,
    TaskStatus,
    TaskSubmission,
)


class TestTask:
    def test_create_task(self) -> None:
        task = Task(name="test", payload={"key": "value"})
        assert task.status == TaskStatus.PENDING
        assert task.id is not None
        assert task.duration is None

    def test_mark_running(self) -> None:
        task = Task(name="test")
        task.mark_running()
        assert task.status == TaskStatus.RUNNING
        assert task.started_at is not None

    def test_mark_completed(self) -> None:
        task = Task(name="test")
        task.mark_running()
        task.mark_completed()
        assert task.status == TaskStatus.COMPLETED
        assert task.duration is not None

    def test_mark_failed(self) -> None:
        task = Task(name="test")
        task.mark_running()
        task.mark_failed("something broke")
        assert task.status == TaskStatus.FAILED
        assert task.error == "something broke"

    def test_is_terminal(self) -> None:
        task = Task(name="test")
        assert not task.is_terminal
        task.mark_running()
        task.mark_completed()
        assert task.is_terminal

    def test_can_retry(self) -> None:
        task = Task(name="test", max_retries=3)
        task.mark_running()
        task.mark_failed("error")
        assert task.can_retry
        task.retries = 3
        assert not task.can_retry

    def test_priority_ordering(self) -> None:
        assert TaskPriority.CRITICAL < TaskPriority.HIGH < TaskPriority.NORMAL


class TestTaskResult:
    def test_success_result(self) -> None:
        result = TaskResult(task_id="123", success=True, output="done")
        assert result.success
        assert result.error is None

    def test_duration_validation(self) -> None:
        with pytest.raises(ValueError, match="non-negative"):
            TaskResult(task_id="123", success=True, duration_ms=-1.0)


class TestTaskSubmission:
    def test_valid_submission(self) -> None:
        sub = TaskSubmission(name="my-task", payload={"a": 1})
        assert sub.name == "my-task"
        assert sub.max_retries == 3

    def test_empty_name_rejected(self) -> None:
        with pytest.raises(ValueError):
            TaskSubmission(name="")
EOF

cat > tests/test_executor.py << 'EOF'
"""Tests for executor implementations."""

from __future__ import annotations

import asyncio

import pytest

from taskflow.errors import RetryExhaustedError
from taskflow.executor.async_executor import AsyncExecutor
from taskflow.executor.retry import RetryExecutor, with_retry
from taskflow.executor.sync_executor import LoggingSyncExecutor, SyncExecutor
from taskflow.models import Task


class TestSyncExecutor:
    def test_execute_with_handler(self, executor: SyncExecutor) -> None:
        task = Task(name="echo", payload={"message": "test"})
        result = executor.execute(task)
        assert result.success
        assert result.output == "test"

    def test_execute_default_handler(self) -> None:
        executor = SyncExecutor()
        task = Task(name="unknown", payload={"a": 1, "b": 2})
        result = executor.execute(task)
        assert result.success
        assert "2 fields" in result.output

    def test_execute_failure(self, executor: SyncExecutor) -> None:
        task = Task(name="fail", payload={})
        result = executor.execute(task)
        assert not result.success
        assert result.error is not None


class TestLoggingSyncExecutor:
    def test_execute_with_logging(self, capsys) -> None:
        executor = LoggingSyncExecutor()
        task = Task(name="test", payload={})
        result = executor.execute(task)
        assert result.success
        captured = capsys.readouterr()
        assert "Starting task" in captured.out


class TestAsyncExecutor:
    @pytest.mark.asyncio
    async def test_async_execute(self) -> None:
        executor = AsyncExecutor()
        task = Task(name="test", payload={"x": 1})
        result = await executor.execute(task)
        assert result.success

    @pytest.mark.asyncio
    async def test_async_batch(self) -> None:
        executor = AsyncExecutor()
        tasks = [Task(name=f"task-{i}", payload={}) for i in range(5)]
        results = await executor.execute_batch(tasks)
        assert len(results) == 5
        assert all(r.success for r in results)


class TestRetryExecutor:
    def test_retry_on_failure(self) -> None:
        call_count = 0

        def counting_handler(payload):
            nonlocal call_count
            call_count += 1
            if call_count < 3:
                raise ValueError("not yet")
            return "success"

        inner = SyncExecutor(handlers={"flaky": counting_handler})
        retry_exec = RetryExecutor(inner, max_retries=5, base_delay=0.01)
        task = Task(name="flaky", payload={})
        result = retry_exec.execute(task)
        assert result.success


class TestWithRetryDecorator:
    def test_decorator_retries(self) -> None:
        attempts = 0

        @with_retry(max_attempts=3, base_delay=0.01)
        def flaky():
            nonlocal attempts
            attempts += 1
            if attempts < 3:
                raise ValueError("not yet")
            return "ok"

        assert flaky() == "ok"
        assert attempts == 3
EOF

cat > tests/test_orchestrator.py << 'EOF'
"""Tests for the orchestrator engine."""

from __future__ import annotations

import pytest

from taskflow.engine.orchestrator import Orchestrator
from taskflow.engine.scheduler import PriorityScheduler, RoundRobinScheduler
from taskflow.errors import QueueFullError, SchedulingError
from taskflow.models import Task, TaskPriority


class TestOrchestrator:
    def test_submit_and_run(self, orchestrator: Orchestrator, sample_task: Task) -> None:
        result = orchestrator.run(sample_task)
        assert result.success
        assert result.output == "hello"

    def test_submit_when_stopped(self, executor) -> None:
        orch = Orchestrator(executor=executor)
        task = Task(name="echo", payload={})
        with pytest.raises(SchedulingError):
            orch.submit(task)

    def test_queue_full(self, executor) -> None:
        orch = Orchestrator(executor=executor, max_queue_size=2)
        orch.start()
        orch.submit(Task(name="a", payload={}))
        orch.submit(Task(name="b", payload={}))
        with pytest.raises(QueueFullError):
            orch.submit(Task(name="c", payload={}))

    def test_cancel_task(self, orchestrator: Orchestrator) -> None:
        task = Task(name="echo", payload={})
        task_id = orchestrator.submit(task)
        assert orchestrator.cancel(task_id)

    def test_metrics(self, orchestrator: Orchestrator, sample_task: Task) -> None:
        orchestrator.run(sample_task)
        metrics = orchestrator.metrics()
        assert metrics["total_completed"] == 1
        assert metrics["executor"] == "sync"

    def test_drain_queue(self, orchestrator: Orchestrator) -> None:
        for i in range(5):
            orchestrator.submit(Task(name="echo", payload={"message": f"msg-{i}"}))
        results = orchestrator.drain_queue()
        assert len(results) == 5
        assert all(r.success for r in results)


class TestPriorityScheduler:
    def test_priority_ordering(self) -> None:
        scheduler = PriorityScheduler()
        low = Task(name="low", priority=TaskPriority.LOW)
        high = Task(name="high", priority=TaskPriority.HIGH)
        normal = Task(name="normal", priority=TaskPriority.NORMAL)

        scheduler.schedule(low)
        scheduler.schedule(high)
        scheduler.schedule(normal)

        assert scheduler.next().name == "high"
        assert scheduler.next().name == "normal"
        assert scheduler.next().name == "low"

    def test_empty_scheduler(self) -> None:
        scheduler = PriorityScheduler()
        assert scheduler.next() is None
        assert scheduler.size == 0


class TestRoundRobinScheduler:
    def test_round_robin_distribution(self) -> None:
        scheduler = RoundRobinScheduler(queue_names=["a", "b"])
        scheduler.schedule(Task(name="a1"), "a")
        scheduler.schedule(Task(name="a2"), "a")
        scheduler.schedule(Task(name="b1"), "b")

        first = scheduler.next()
        second = scheduler.next()
        assert {first.name, second.name} == {"a1", "b1"}

    def test_empty_round_robin(self) -> None:
        scheduler = RoundRobinScheduler()
        assert scheduler.next() is None
EOF

commit "Add test suite" "2024-07-05T10:00:00"

# ============================================================
# Commit 12: Tech debt audit (TODO/FIXME/NOTE comments)
# ============================================================

cat >> taskflow/models.py << 'APPENDEOF'

# TODO: Add task dependency tracking (task A depends on task B).
# FIXME: TaskStatus enum allows invalid transitions (e.g., COMPLETED -> RUNNING).
# NOTE: Pydantic models are used for API serialization while dataclasses are
# used for domain objects. This avoids coupling the domain to the API framework.
APPENDEOF

cat >> taskflow/errors.py << 'APPENDEOF'

# TODO: Add error codes as an enum instead of string constants.
# FIXME: TimeoutError shadows the built-in TimeoutError — consider renaming.
# NOTE: The 3-deep hierarchy was chosen to match common real-world patterns.
# Flattening to 2 levels would simplify error handling but lose specificity.
APPENDEOF

cat >> taskflow/config.py << 'APPENDEOF'

# TODO: Add support for .env files via python-dotenv.
# FIXME: No config validation on startup — invalid values cause runtime errors.
# NOTE: Environment variable prefix TASKFLOW_ was chosen to avoid collisions
# with common framework variables (e.g., PORT, DEBUG).
APPENDEOF

cat >> taskflow/executor/base.py << 'APPENDEOF'

# TODO: Add async Protocol variant (AsyncExecutorProtocol).
# NOTE: Having both Protocol and ABC in the same file is intentional —
# it demonstrates two valid approaches to interface definition in Python.
# In production, choose one pattern and stick with it.
APPENDEOF

cat >> taskflow/executor/sync_executor.py << 'APPENDEOF'

# TODO: Add handler timeout to prevent individual tasks from blocking forever.
# FIXME: LoggingMixin uses print() — should use structured logger instead.
# NOTE: Diamond MRO (LoggingSyncExecutor -> LoggingMixin -> SyncExecutor -> Executor)
# works because Python C3 linearization handles this case correctly.
APPENDEOF

cat >> taskflow/executor/async_executor.py << 'APPENDEOF'

# TODO: Add semaphore-based concurrency limiting for execute_batch.
# FIXME: _running set is not thread-safe if accessed from multiple threads.
# NOTE: asyncio.gather with return_exceptions=True prevents one failure
# from cancelling the entire batch, which is the desired behavior.
APPENDEOF

cat >> taskflow/engine/orchestrator.py << 'APPENDEOF'

# TODO: Add event hooks for task lifecycle transitions (on_submit, on_complete).
# FIXME: drain_queue processes tasks sequentially — should use worker pool.
# NOTE: The orchestrator owns the task registry. In a distributed setup,
# this would need to be backed by Redis or a database.
APPENDEOF

cat >> taskflow/engine/scheduler.py << 'APPENDEOF'

# TODO: Add weighted round-robin scheduling.
# FIXME: PriorityScheduler counter can overflow on very long-running instances.
# NOTE: Both schedulers implement schedule/next without a shared base class.
# This is intentional duck-typing — they satisfy the same structural protocol.
APPENDEOF

cat >> taskflow/api/middleware.py << 'APPENDEOF'

# TODO: Add request correlation ID header (X-Request-ID).
# FIXME: RateLimitMiddleware stores state in memory — won't work with multiple workers.
# NOTE: Both middleware classes have the same dispatch method signature.
# This is the standard Starlette middleware pattern.
APPENDEOF

cat >> taskflow/api/routes.py << 'APPENDEOF'

# TODO: Add pagination for task listing endpoint.
# FIXME: No authentication or authorization on any endpoint.
# NOTE: Six endpoints in one file is typical for small FastAPI services.
# Split into separate routers when the API grows beyond 10 endpoints.
APPENDEOF

commit "Tech debt audit with TODO/FIXME/NOTE markers" "2024-07-10T10:00:00"

echo ""
echo "Python test repo created at $REPO_DIR"
echo "  - $(find "$REPO_DIR" -name '*.py' | wc -l | tr -d ' ') Python files"
echo "  - $(cat $(find "$REPO_DIR" -name '*.py') 2>/dev/null | wc -l | tr -d ' ') lines of Python"
echo "  - $(git -C "$REPO_DIR" log --oneline | wc -l | tr -d ' ') commits"
echo ""
echo "Run: go run ./cmd/atlaskb index --force $REPO_DIR"
