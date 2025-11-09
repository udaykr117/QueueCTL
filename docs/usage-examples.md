# Usage Examples

Complete CLI command examples with actual outputs for QueueCTL.

## 1. Enqueue Jobs

Enqueue a simple job:
```bash
./queuectl enqueue '{"id":"job-1","command":"echo Hello World"}'
```
Output:
```
Job enqueued successfully: job-1
```

Enqueue a job with timeout:
```bash
./queuectl enqueue '{"id":"job-2","command":"sleep 10","timeout":5}'
```
Output:
```
Job enqueued successfully: job-2
```

Enqueue a job that will fail (for testing retries):
```bash
./queuectl enqueue '{"id":"job-3","command":"false","max_retries":3}'
```
Output:
```
Job enqueued successfully: job-3
```

Job JSON format:
```json
{
  "id": "unique-job-id",      // Required
  "command": "shell command",  // Required
  "max_retries": 3,            // Optional (default: 3)
  "timeout": 300              // Optional, in seconds (default: 300)
}
```

---

## 2. Start Workers

Start multiple workers for parallel processing:
```bash
./queuectl worker start --count 2
```
Output:
```
2025/11/09 12:56:47 Started 2 workers (PID: 79011)
2025/11/09 12:56:47 [worker-2] Started
2025/11/09 12:56:47 [worker-1] Started
2025/11/09 12:56:47 [worker-2] Processing job: job-1 (command: echo Hello World)
2025/11/09 12:56:47 [worker-2] Job job-1 completed successfully
2025/11/09 12:56:47 [worker-2] Processing job: job-2 (command: sleep 10)
2025/11/09 12:56:47 [worker-1] Processing job: job-3 (command: false)
2025/11/09 12:56:47 [worker-1] Job job-3 failed: command exited with code 1: 
2025/11/09 12:56:47 [worker-1] Job job-3 will retry in 2s (attempt 1/3)
```

Stop workers:
```bash
./queuectl worker stop
```
Output:
```
Workers stopped successfully
```

---

## 3. Check Queue Status

View summary of all jobs and active workers:
```bash
./queuectl status
```
Output:
```
Job Queue Status
===============
Pending:    0
Processing: 0
Completed:  1
Failed:     0
Dead:       2

Active Workers: 0
```

---

## 4. List Jobs

List all jobs:
```bash
./queuectl list
```
Output:
```
ID                   STATE           ATTEMPTS   MAX_RETRIES CREATED_AT               
--------------------------------------------------------------------------------
job-1                completed       1          3          2025-11-09T12:56:43Z     
job-2                dead            3          3          2025-11-09T12:56:44Z     
job-3                dead            3          3          2025-11-09T12:56:45Z     
```

List jobs by state:
```bash
./queuectl list --state pending
```
Output:
```
No jobs found with state: pending
```

```bash
./queuectl list --state completed
```
Output:
```
ID                   STATE           ATTEMPTS   MAX_RETRIES CREATED_AT               
--------------------------------------------------------------------------------
job-1                completed       1          3          2025-11-09T12:56:43Z     
```

```bash
./queuectl list --state failed
```
Output:
```
No jobs found with state: failed
```

---

## 5. View Job Details

View complete job information including output:
```bash
./queuectl show job-2
```
Output:
```
Job Details
================================================================================
ID:                  job-2
Command:             sleep 10
State:               dead   
Attempts:            3
Max Retries:         3
Timeout:             5 seconds
Created At:          2025-11-09T12:57:00Z
Updated At:          2025-11-09T12:57:00Z
Last Error:          job timeout after 5s:

Output
--------------------------------------------------------------------------------
(No output available)


```

---

## 6. Dead Letter Queue (DLQ)

List jobs in DLQ:
```bash
./queuectl dlq list
```
Output:
```
Dead Letter Queue Jobs (2)
================================================================================
ID                   STATE           ATTEMPTS   MAX_RETRIES CREATED_AT               
--------------------------------------------------------------------------------
job-2                dead            3          3          2025-11-09T13:05:37Z     
job-3                dead            3          3          2025-11-09T13:05:44Z     
```



Retry a job from DLQ:
```bash
./queuectl dlq retry job-3
```
Output:
```
Job job-3 has been reset to pending state and will be retried
```

---

## 7. Configuration Management

List all configurations:
```bash
./queuectl config list
```
Output:
```
No configuration set
```


Set configuration:
```bash
./queuectl config set max-retries 5
```
Output:
```
Configuration 'max-retries' set to '5'
```

Get configuration:
```bash
./queuectl config get max-retries
```
Output:
```
5
```

Available configuration keys:
- `max-retries`: Default max retry attempts (default: 3)
- `backoff-base`: Exponential backoff base (default: 2.0)
- `default-job-timeout`: Default timeout in seconds (default: 300)
- `dashboard-port`: Dashboard server port (default: 8080)

---

## 8. Web Dashboard

Start dashboard on custom port:
```bash
./queuectl dashboard --port 9090
```
Output:
```
Dashboard server starting on http://localhost:9090
```

Start dashboard on default port (8080):
```bash
./queuectl dashboard
```
Output:
```
Dashboard server starting on http://localhost:8080
```

Access at `http://localhost:8080` (or your custom port) to view real-time metrics, queue status, and execution history.
