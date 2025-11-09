# QueueCTL

A CLI based background job queue system that manages background jobs with worker processes, handles retries using exponential backoff, and maintains a Dead Letter Queue (DLQ) for permanently failed jobs. Features include timeout handling, metrics tracking, and a minimal web dashboard for monitoring.

## Demo Video

A working CLI demonstration of QueueCTL:

📹 [View Demo Video on Google Drive](https://drive.google.com/drive/folders/1UtA7SOAPJ5MmbPX9fPvhuIoBwRABDpOi?usp=sharing)

## Table of Contents

- [Demo Video](#demo-video)
- [Setup Instructions](#setup-instructions)
- [Usage Examples](#usage-examples)
- [Architecture Overview](#architecture-overview)
- [Assumptions & Trade-offs](#assumptions--trade-offs)
- [Testing Instructions](#testing-instructions)

## Setup Instructions

### Prerequisites

- Go 1.21 or later
- SQLite3 (usually pre-installed on most systems)
- Bash shell (for running test script)

### Installation

1. **Clone the repository**
   ```bash
   git clone https://github.com/udaykr117/queuectl.git
   cd queuectl

   ```

2. **Install dependencies:**
   ```bash
   go mod download
   ```

3. **Build the application:**
   ```bash
   go build -o queuectl .
   ```
   
   Or use the Makefile:
   ```bash
   make build
   ```

4. **Verify installation:**
   ```bash
   ./queuectl --help
   ```

### Data Directory

By default, queuectl stores its database in a `data/` directory relative to the executable. You can override this using the `QUEUECTL_DATA_DIR` environment variable:

```bash
export QUEUECTL_DATA_DIR=/path/to/custom/data
./queuectl status
```


## Usage Examples

For detailed CLI command examples with actual outputs, see [docs/usage-examples.md](docs/usage-examples.md)

## Architecture Overview


### High-Level Architecture

```
┌─────────────┐
│   CLI       │  User commands (enqueue, list, status, etc.)
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  Main App   │  Command routing 
└──────┬──────┘
       │
       ├──────────────┬──────────────┬──────────────┐
       ▼              ▼              ▼              ▼
┌─────────────┐ ┌──────────┐    ┌──────────┐    ┌──────────┐
│   Storage   │ │  Worker  │    │  Config  │    │ Dashboard│
└──────┬──────┘ └────┬─────┘    └──────────┘    └──────────┘
       │             │
       ▼             ▼
┌─────────────┐ ┌─────────────┐
│  SQLite DB  │ │  Job Exec   │
│  (jobs.db)  │ │  (sh -c)    │
└─────────────┘ └─────────────┘
```

### Job Lifecycle

1. **Pending**: Job is enqueued and waiting to be processed
2. **Processing**: Job is currently being executed by a worker
3. **Completed**: Job executed successfully
4. **Failed**: Job failed but may be retried
5. **Dead**: Job exceeded max retries and moved to DLQ

### Data Persistence

- **SQLite Database**: All job data, metrics, and execution history are stored in `data/jobs.db`
- **WAL Mode**: Database uses Write-Ahead Logging for better concurrency
- **Persistence**: Data survives application restarts

### Worker Logic

- **Worker Pool**: Manages multiple concurrent workers
- **Job Locking**: Uses database-level locking to prevent duplicate processing
- **Exponential Backoff**: Failed jobs retry with increasing delays (base^attempts seconds)
- **Timeout Handling**: Jobs can specify timeout; default is 5 minutes
- **Output Capture**: Job stdout/stderr is captured and stored

### Retry Mechanism

When a job fails:
1. Increment attempt counter
2. If attempts < max_retries:
   - Calculate backoff delay: `base^attempts` seconds
   - Set `next_retry_at` timestamp
   - Move job back to `pending` state
3. If attempts >= max_retries:
   - Move job to `dead` state (DLQ)

### Metrics & Execution Stats

The system tracks:
- `jobs_processed`: Total jobs processed
- `jobs_failed`: Total jobs that failed
- `jobs_timeout`: Total jobs that timed out
- Execution history with duration, success status, and error messages


## Assumptions & Trade-offs

### Assumptions

1. **Single Machine Deployment**: Designed for single-machine use, not distributed
2. **Shell Commands**: Jobs execute as shell commands (`sh -c`)
3. **SQLite Sufficiency**: SQLite provides adequate performance for moderate job volumes
4. **File System Access**: Workers have access to execute arbitrary commands
5. **No Authentication**: No built-in authentication/authorization

### Trade-offs & Simplifications

1. **SQLite Over JSON or Any other DB**:  SQLite chosen for simplicity and zero-configuration
   - **Trade-off**: Less scalable than PostgreSQL/MySQL and slightly heavier than using a simple JSON file
   - **Benefit**: Provides ACID transactions and reliable persistence without requiring a separate database server



2. **No Job Priority**: Jobs processed in FIFO order
   - **Trade-off**: Cannot prioritize urgent jobs
   - **Benefit**: Predictable behavior and minimal implementation complexity

3. **Synchronous Execution**: Workers block while executing jobs
   - **Trade-off**: One job per worker at a time
   - **Benefit**: Simpler error handling and resource management

4. **Configuration Storage**: Config stored in same database as jobs
   - **Trade-off**: Requires database initialization
   - **Benefit**: Unified data storage, fewer moving parts


## Testing Instructions

### Running the Test Suite

The project includes a comprehensive test script that validates all functionality:

```bash
# Make the test script executable
chmod +x test.sh

# Run all tests
./test.sh
```

The test script will:
1. Build the application
2. Run tests in a separate test database (`test_data/`)
3. Validate all 10 test scenarios:
   - Basic job completion
   - Failed job retries with backoff
   - Multiple workers without overlap
   - Invalid command handling
   - Job data persistence
   - DLQ retry functionality
   - Configuration management
   - Timeout handling
   - Job output logging
   - Metrics and execution stats

### Test Output

The script provides colored output:
- ✓ Green: Passed tests
- ✗ Red: Failed tests
- Summary at the end with pass/fail counts



## Additional Resources

- [docs/usage-examples.md](docs/usage-examples.md) - Complete CLI command examples with outputs
- See `./queuectl --help` for full command reference
