# Post-generation corrections for cmd/sandbin/openapi.json (plan 0030, ruling R4).
# `make openapi` pipes apispec v0.5.11 through this filter because the generator
# verifiably cannot express these nine things itself:
#
# 1. Drop the document-level `security` block. It was an inheritance device, never
#    sanctioned by docs/decisions/0030-api-servemux-openapi.md; security belongs on
#    each operation.
# 2. Spell /api/health GET+HEAD with explicit `security: []`. health registers a
#    bare handler, and v0.5.11 can only attach security mappings to a resolvable
#    middleware identity, so no per-operation "public" line can be generated.
# 3. Spell GET /api/bins/{hash} with the explicit optional requirement
#    [{}, {binBasic: []}] — the line the document-level block used to imply, and
#    apispec has no per-operation way to emit an empty {} requirement.
# 4. Drop the 401s from GET /api/bins, POST /api/bins and POST /api/key. Only
#    binAuthorized answers 401, and only inspect/patch/delete call it; the
#    generator's middleware-flow attribution pools that 401 onto every
#    withAPIKey-wrapped operation.
# 5. POST /api/bins requestBody is `required: false`: bodyless create is pinned
#    behavior (decodeBinRequest(..., true)); the generator marks every body it sees
#    required.
# 6. Strip every `"Extensions": null` from parameters — a leak of the generator's
#    internal struct, not a field of OpenAPI 3.1.
# 7. (Implicit) jq's own output ends with a trailing newline, which the generator
#    omits.
# 8. Replace GET /openapi.json's phantom `default` response with the pinned 200
#    `application/json`. apispec v0.5.11 sees the registration but not what the
#    handler writes — serveOpenAPI writes the embedded bytes through no response
#    pattern it can match — so the scan can only answer "no response found". The
#    body schema is deliberately an opaque object: the document describes itself.
# 9. Spell the components `archiveAnswer` schema. The bin-content PUT paths are out
#    of the document's scope by the decision doc, so apispec never sees the
#    writeJSON call that answers {"created":N,"replaced":M}; the contract tests
#    (openapi_test.go) validate that live answer against this checked-in schema.

del(.security)
| .paths["/api/health"].get.security = []
| .paths["/api/health"].head.security = []
| .paths["/api/bins/{hash}"].get.security = [{}, { "binBasic": [] }]
| del(.paths["/api/bins"].get.responses["401"],
      .paths["/api/bins"].post.responses["401"],
      .paths["/api/key"].post.responses["401"])
| .paths["/api/bins"].post.requestBody.required = false
| .paths["/openapi.json"].get.responses = {
    "200": {
      "description": "OK",
      "content": {
        "application/json": {
          "schema": {
            "type": "object",
            "description": "The OpenAPI 3.1 document itself: the committed cmd/sandbin/openapi.json, served byte for byte."
          }
        }
      }
    }
  }
| .components.schemas.archiveAnswer = {
    "type": "object",
    "description": "archiveAnswer is the JSON a successful archive upload answers: the store's own created and replaced counts for the extracted entries.",
    "properties": {
      "created": {
        "type": "integer"
      },
      "replaced": {
        "type": "integer"
      }
    }
  }
| walk(if type == "object" and has("Extensions") and .Extensions == null
       then del(.Extensions) else . end)
