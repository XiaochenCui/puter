- Feature Name: Client Replica Filesystem
- Status: Draft
- Date: 2025-08-21

## Summary

**Client Replica Filesystem** is a mechanism that keeps a **full replica** of a user’s filesystem tree on the client and regularly sync updates from the server. This feature allows:

* Rapid file system operations for read-only APIs such as `stat`, `readdir`, and `search`. No network round trips are needed.
* Lower network I/O along with reduced database and CPU load on the server.

## Motivation

The **puter filesystem** is a critical component of Puter, it provides a POSIX-like filesystem interface to `puter-js` and powers the filesystem operations in the GUI web client. APIs provided by the filesystem include:

- Read-only APIs: `stat`, `readdir`, `search`.
- Write APIs: `mkdir`, `write`, `copy`, `move`, `rename`, `delete`, etc.

Currently, all of these operations are handled through the synchronous HTTP API and suffer from latency issues caused by network round trips and database index contention. For example, when a user opens a folder in the GUI web client, the request will go all the way to database to find what's inside the folder. There are 20 million filesystem entries in the database and the latency will keep increasing as the number of files grows.

To tackle this issue, we propose maintaining a **full replica** of the filesystem rooted at the user’s home directory on the client (e.g., for user Tim, all filesystem nodes under `/Tim` are stored locally). This allows users to perform read-only operations on the client replica without waiting for a server response. Updates to the filesystem will be fetched from the server periodically.

![](assets/20250910_113939_puter-client_replica.drawio.svg)

## Implementation

### Data Structure

**Merkle Tree** is used to quickly compare two file system trees and synchronize them by sending only the differences.

In our implementation, we use two key ideas:

1. **Bidirectional Nodes**
   Each node stores references to both its parent and children.

   * **Top-down traversal**: used for tree comparison and path lookup.
   * **Bottom-up traversal**: used to recalculate hashes when a node is updated.

2. **Heap (Index by UUID)**
   We maintain a heap-like structure (UUID → node map) to:

   * Enable fast node lookups by UUID.
   * Prevent duplicate nodes in the tree.

> A Merkle tree is a hash tree where leaves are hashes of the values of individual nodes. Parent nodes higher in the tree are hashes of their respective children. The principal advantage of Merkle tree is that each branch of the tree can be checked independently without requiring nodes to download the entire tree or the entire data set. Moreover, Merkle trees help in reducing the amount of data that needs to be transferred while checking for inconsistencies among replicas. For instance, if the hash values of the root of two trees are equal, then the values of the leaf nodes in the tree are equal and the nodes require no synchronization. If not, it implies that the values of some replicas are different. In such cases, the nodes may exchange the hash values of children and the process continues until it reaches the leaves of the trees, at which point the hosts can identify the nodes that are “out of sync”.

### Client-Replica Initialization

Both initialization and synchronization are done via websocket to save network traffic.

### Client-Replica Synchronization

### Client-side Replica

#### Initialization

The client will fetch all nodes under a user's home directory once the user is logged in by `puter.fs.replica.fetch(<path>)`.

HTTP endpoint: `/api/fs/replica/fetch`

POST arguments:
- `path`: The path to fetch the replica tree for (e.g., "/Tim")

#### Endpoint: `/api/fs/replica/fetch`

##### Request

**Method:** `POST`  
**Content-Type:** `application/json`

##### Request Body
```json
{
  "path": "/Tim"
}
```

##### Parameters
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `path` | string | Yes | The path to fetch the replica tree for (e.g., "/Tim") |

##### Success Response (200 OK)
```json
{
  "success": true,
  "data": {
    "path": "/Tim",
    "hash": "<hash>",
    "children": [
      {
        "name": "<name>",
        "hash": "<hash>",
        "metadata": {
          ...
        },
        "children": [
          {
            "name": "<name>",
            "hash": "<hash>",
            "metadata": {
              ...
            }
          }
        ]
      }
    ]
  }
}
```

##### Error Response (500 Internal Server Error)
```json
{
  "success": false,
  "error": {
    "code": "INTERNAL_ERROR",
    "message": "Failed to build filesystem tree"
  }
}
```

message can be:

- `SERVICE_UNAVAILABLE`: FS-Tree Manager is not available.
- `TREE_UNAVAILABLE`: FS-Tree Manager is not able to provide this tree.

There are some details to consider:

- Client Memory Usage: In the POC implementation, a tree consists of 100K nodes takes around 10MB browser memory. A hard limit of 20MB (i.e., 200K nodes) can be set at server side to avoid taking too much memory. When a user has too much file nodes under his home directory, server can send an error back to the client.
- Initialization Time: According to the data size mentioned above, the initialization will finish within 1 second. But it's still great to put it in a background task to avoid blocking the UI thread.
- Permission: Permission check should be enforced on both client side and server side. A user can only fetch the tree started from his home directory. A simpler design is to remove the args from `puter.fs.fetch_tree` and make it "fetch all files for the current user".

#### File System Operations Upon Fetching

To make the system consistent, the local replica will work with all existing file system APIs except `read` and `write`. A simple implementation is to have a switch branch for local replica:

```js
const readdir = async function (...args) {
    // ... (existing code)

    if (this.local_replica.available) {
        return this.local_replica.readdir(options.path);
    }

    // ... (existing code which fetches from server)
}
```

### Synchronize Changes

Since CRDT is not used, synchronization between client and server is one-way — the client only fetches changes from the server. Each node in the tree includes a `hash` field that is the hash of **all its children's hashes + its own metadata**. So its safe to say 2 trees are the same if and only if their root nodes have the same hash.

Synchronize is done via HTTP endpoint `/api/fs/replica/sync`.

Here is an example of a complete cycle of sync:

1. Client initiates a sync process by sending a request to the server:

```json
{
  "pull_requests": [
    {
      "path": "/Tim",
      "hash": "<hash>"
    }
  ]
}
```

2. The server compares this tree with its own replica:

```json
{
  "path": "/Tim",
  "hash": "<hash>",
  "children": [...]
}
```

Where `children` contains some updates from other sessions, which makes the server have a different `hash`.

3. The server sends the following response to the client:

```json
{
  "push_requests": [
    {
      "path": "/Tim",
      "hash": "<hash>",
      "children": [
        {
          "name": "same_tree",
          "hash": "<hash>"
        },
        {
          "name": "client_newer",
          "hash": "<hash>"
        },
        {
          "name": "server_newer",
          "hash": "<hash>"
        },
        {
          "name": "server_only",
          "hash": "<hash>"
        }
      ]
    }
  ]
}
```

An empty `push_requests` means there are no differences to sync:

```json
{
  "push_requests": []
}
```

4. The client receives this `push_requests` and compare it with its own replica:

```json
{
  "path": "/Tim",
  "hash": "<hash>",
  "children": [
    {
      "name": "same_tree",
      "hash": "<hash>"
    },
    {
      "name": "client_newer",
      "hash": "<hash>"
    },
    {
      "name": "server_newer",
      "hash": "<hash>"
    },
    {
      "name": "client_only",
      "hash": "<hash>"
    }
  ]
}
```

Explanation:

* `/Tim/same_tree` — the contents are identical on both the client and the server.
* `/Tim/client_newer` — the client version is newer than the server version (different `<hash>`).
* `/Tim/server_newer` — the server version is newer than the client version (different `<hash>`).
* `/Tim/server_only` — exists on the server but not on the client.
* `/Tim/client_only` — exists on the client but not on the server.

5. The client sends the following request to the server:

```json
{
  "pull_requests": [
    {
      "path": "/Tim",
      "hash": "<hash>",
      "children": [
        {
          "name": "server_newer",
          "hash": "<hash>"
        },
        {
          "name": "server_only",
          "hash": "<hash>"
        }
      ]
    }
  ]
}
```

6. The server responds with `push_requests`:

```json
{
  "push_requests": [
    {
      "path": "/Tim",
      "hash": "<hash>",
      "children": [
        {
          "name": "server_newer",
          "hash": "<hash>",
          "metadata": {
            "uuid": "<uuid>",
            "is_dir": "<is_dir>",
            "...": "..."
          }
        },
        {
          "name": "server_only",
          "hash": "<hash>",
          "metadata": {
            "uuid": "<uuid>",
            "is_dir": "<is_dir>",
            "...": "..."
          }
        }
      ]
    }
  ]
}
```

- Both server and client will store a `last_update_time` for their own replica, and the client will send it to the server in the sync request. For now, the server push changes even the client replica is newer. We will track the frequency of "sync request from a newer replica" and decide whether to reject the sync request from a newer replica.

### FS-Tree Manager

Just a standalone service that manages the FS-Tree.

TODO: Add more details.

## Scalability

### First Stage - Single Instance

The first stage is to have a single instance of the FS-Tree Manager. We will use following strategies to avoid out-of-memory (OOM) issues:

- On server initialization, don't cache any FS tree.
- Only build FS tree when a request comes in.
- Evict FS tree from memory when it's not used for 10 minutes. Use `last_access_time` for the eviction logic.
- Set a hard limit of 4GB for the FS-Tree Manager, reject to create new FS tree when the memory usage reaches the limit.

### Second Stage - Partitioned FS-Tree Manager

Use consistent hashing (by userid) to partition the FS-Tree Manager.

TODO: Add more details on how to add/remove instances.

TODO: We may need a GUI control panel for partition management.

## Fault Tolerance

TODO: 

scenario 1: FS-Tree Manager is unavailable on all APIs.

scenario 2: FS-Tree Manager is only unavailable on fetch/sync APIs.

scenario 3: FS-Tree Manager is only unavailable on fs update APIs.

## Metrics

### Change Propagation Time

The time it takes for a change made on one client (such as creating, renaming, or deleting a file or folder) to appear and become visible on another client.

The **Change Propagation Time** on original synchronize model is negligible. But with the new model, the **Average Change Propagation Time** will be 6 seconds. And the **Maximum Change Propagation Time** will be 15 seconds when internal services are functioning normally.

## Optimization in the Future

- In the initial implementation, FS-Tree Manager can only serve a request if it holds the entire FS tree in memory. We can optimize it by only holding the root node in memory and fetch the children on demand.
- Replace some part of HTTP API with websocket to reduce round trips and latency.

## Failure Scenarios

### FS-Tree Manager Failure

### FS-Update Notification Failure

## Alternatives and Trade-offs

### Last-Updated Time for "Stale Replica Fetch"

### Alternative Storage Models
