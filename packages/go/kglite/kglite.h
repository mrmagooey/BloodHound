#ifndef KGLITE_H
#define KGLITE_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/* Opaque graph handle. */
typedef struct KgHandle KgHandle;

/* Create a new empty graph. Returns NULL on allocation failure. */
KgHandle *kg_new(void);

/* Free a graph handle. Passing NULL is a no-op. */
void kg_free(KgHandle *handle);

/* Load a graph from a .kgl file. Returns NULL on error; call kg_last_error() for details. */
KgHandle *kg_load(const char *path);

/* Save a graph to a .kgl file. Returns 0 on success, -1 on error. */
int kg_save(const KgHandle *handle, const char *path);

/*
 * Execute a Cypher query.
 *
 * - handle:      graph handle (must not be NULL)
 * - query:       NUL-terminated Cypher query string
 * - params_json: NUL-terminated JSON object of query parameters, or NULL for none
 * - out:         on success, written with a pointer to a NUL-terminated JSON string;
 *                the caller must free this with kg_free_string()
 *
 * Returns 0 on success, -1 on error. Call kg_last_error() on failure.
 *
 * Result JSON format: {"columns": ["col1", ...], "rows": [[val, ...], ...]}
 */
int kg_cypher(KgHandle *handle, const char *query, const char *params_json, char **out);

/*
 * Execute multiple Cypher queries in a single lock acquisition.
 *
 * - queries_json: JSON array of objects [{"query":"...","params":{...}}, ...]
 * - out: receives a JSON array of result objects (same format as kg_cypher)
 *
 * Returns 0 on success, -1 on error.
 */
int kg_cypher_batch(KgHandle *handle, const char *queries_json, char **out);

/*
 * Bulk-create edges by node index, bypassing Cypher for maximum throughput.
 *
 * - edges_json: JSON array of edge specs:
 *   [{"src": <node_idx>, "dst": <node_idx>, "type": "EdgeType", "props": {...}}, ...]
 * - skip_existing: if non-zero, skip duplicate-edge checks (faster for fresh edges)
 * - out: receives a JSON string {"created": <count>}
 *
 * Returns 0 on success, -1 on error.
 */
int kg_create_edges_batch(KgHandle *handle, const char *edges_json, int skip_existing, char **out);

/* Free a string allocated by kg_cypher or kg_cypher_batch. */
void kg_free_string(char *s);

/*
 * Return the last error message for this thread, or NULL if there was no error.
 * The pointer remains valid until the next FFI call on this thread.
 */
const char *kg_last_error(void);

/* Memory statistics reported by the tracking allocator. */
typedef struct {
    uint64_t current_bytes; /* current live Rust heap bytes */
    uint64_t peak_bytes;    /* peak live Rust heap bytes since process start */
    uint64_t total_allocs;  /* total allocations since process start */
} KgMemStats;

/* Return current Rust heap statistics. Fields are zero in non-FFI builds. */
KgMemStats kg_memory_stats(void);

#ifdef __cplusplus
}
#endif

#endif /* KGLITE_H */
