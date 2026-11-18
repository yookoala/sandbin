# Post-generation corrections for cmd/sandbin/openapi.json (plan 0030, ruling R4).
# `make openapi` pipes apispec v0.5.11 through this filter because the generator
# verifiably cannot express these seventeen things itself:
#
# 1. Drop the document-level `security` block. It was an inheritance device, never
#    sanctioned by docs/decisions/0030-api-servemux-openapi.md; security belongs on
#    each operation.
# 2. Spell every key-free operation — /api/health GET+HEAD and /openapi.json GET —
#    with explicit `security: []`, so the document spells "no requirement" the same
#    way for all three instead of leaving one of them to silence. health and the
#    document register bare handlers, and v0.5.11 can only attach security mappings
#    to a resolvable middleware identity, so no per-operation "public" line can be
#    generated.
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
# 9. Spell the components `archiveAnswer` schema and its required list. The bin-content
#    PUT paths are out of the document's scope by the decision doc, so apispec never
#    sees the writeJSON call that answers {"created":N,"replaced":M}; the contract
#    tests (openapi_test.go) validate that live answer against this checked-in schema.
#    requiredFromJSONTags in apispec.yaml reaches generated components only, so the
#    required list of this hand-spelled one belongs here too.
# 10. Rewrite every operation's summary, description and operationId into client-facing
#     contract prose. The generator copies Go doc-comment prose — internal names
#     (withAPIKey, requireKey, apiBinPathRE, UpdateBinMeta, decodeBinRequest) and a
#     reference to the retired POST / — straight into the published document; its
#     `overrides:` block is inert on v0.5.11, and naming.operationId can only spell
#     receiver-method ids like server.apiCreateBin or headApiHealth. The rewrite keeps
#     the substance — charging, bcrypt limits, tri-state PATCH, opaque cursor — and
#     names the nine operations createBin…getOpenAPIDocument.
# 11. Give the plain-text error responses example values carrying the pinned refusal
#     sentences (api.go and handlers.go), per operation and per status — only for
#     statuses that operation can actually answer, and as an `example` where one
#     sentence is possible or `examples` where several are.
# 12. Spell the Location header on POST /api/bins 201 (the handler sets it to
#     /api/bins/{hash}): apispec documents request header reads as parameters but
#     nothing about w.Header().Set on the response side.
# 13. Spell the Retry-After header on every 429 in the document (requireKey sets it to
#     the lockout's remaining seconds) — same reason.
# 14. Write `additionalProperties: false` into the RawMessage request schema: apispec's
#     config Schema type rejects a boolean additionalProperties outright (the yaml load
#     fails), so the flag that mirrors decodeBinRequest's DisallowUnknownFields — an
#     unrecognized field really is a 400 — can only land in this filter.
# 15. Spell both /api index operations with explicit `security: []` — the index registers a
#     bare handler, exactly correction 2's family (docs/decisions/0031-api-index-endpoint.md).
# 16. Replace each /api index operation's phantom `default` response with the pinned 200 —
#     correction 8's reason again: apiIndex is outside the response-pattern scan. The GET 200
#     carries a placeholder `application/json` `type: object` schema on purpose: plan 0031's
#     second task promotes it to a real component schema and adds the live-body honesty pin,
#     so this clause is deliberately transient. The HEAD 200 carries no content: HEAD answers
#     no body.
# 17. Name the two index operations in the correction 10 style: client-facing summaries and
#     descriptions, and the operationIds apiIndex and headAPIIndex.

def op($id; $summary; $description):
  .operationId = $id | .summary = $summary | .description = $description;
def textexample($path; $method; $status; $value):
  .paths[$path][$method].responses[$status].content["text/plain; charset=utf-8"].example = $value;
def plain400examples($path; $method; $map):
  .paths[$path][$method].responses["400"].content["text/plain; charset=utf-8"].examples = $map;
def retryAfter: {
  "Retry-After": {
    "description": "Whole seconds until the requesting address's API-key lockout expires.",
    "schema": { "type": "integer" }
  }
};

del(.security)
| .paths["/api"].get.security = []
| .paths["/api"].head.security = []
| .paths["/api/health"].get.security = []
| .paths["/api/health"].head.security = []
| .paths["/openapi.json"].get.security = []
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
| .paths["/api"].get.responses = {
    "200": {
      "description": "OK",
      "content": {
        "application/json": {
          "schema": {
            "type": "object",
            "description": "The API index: the server's version stamp, its published endpoints with a description per method and a full URL built from the request's own context, and an openapi pointer to this document."
          }
        }
      }
    }
  }
| .paths["/api"].head.responses = {
    "200": {
      "description": "OK"
    }
  }
| .components.schemas.archiveAnswer = {
    "type": "object",
    "description": "archiveAnswer is the JSON a successful archive upload answers: the store's own created and replaced counts for the extracted entries.",
    "required": ["created", "replaced"],
    "properties": {
      "created": {
        "type": "integer"
      },
      "replaced": {
        "type": "integer"
      }
    }
  }

# Correction 14, and correction 6 on the generated half.
| .components.schemas.RawMessage.additionalProperties = false
| walk(if type == "object" and has("Extensions") and .Extensions == null
       then del(.Extensions) else . end)

# Corrections 10 and 17: the eleven operations, in the document's own path order. Each summary
# is one client-facing line; each description states the contract the handler keeps.
| .paths["/api"].get |= op(
    "apiIndex";
    "Index of this API: every published endpoint in one JSON body.";
    "Answers 200 application/json with the server's version stamp, every published endpoint with a full URL built from the request's own host and scheme and a description for each method, and an openapi sibling pointing at this document. Needs no API key, touches no store and charges nothing, so it answers even while the requesting address is blocked.")
| .paths["/api"].head |= op(
    "headAPIIndex";
    "Index of this API: every published endpoint in one JSON body.";
    "Answers 200 with no body. Needs no API key, touches no store and charges nothing, so it answers even while the requesting address is blocked.")
| .paths["/api/bins"].post |= op(
    "createBin";
    "Create a bin from a JSON body.";
    "Answers 201 with the new bin's object and a Location naming /api/bins/{hash} for it. The body may name any mix of title, description and password: an absent field and one given as null or \"\" both make the empty value. A title over 200 bytes or carrying control characters, a description over 2000 bytes or carrying control characters, or a password over the 72 bytes bcrypt can hash is refused with 400 and creates nothing. Key-checked: a wrong API key answers 403 and costs the requesting address a lockout step.")
| .paths["/api/bins"].get |= op(
    "listBins";
    "List this instance's bins, newest first.";
    "Answers 200 with a page of bin objects and, only when a further page exists, the next_cursor that resumes the walk after the page's last one. limit defaults to 30 and clamps to 100; a non-integer or a limit below 1 answers 400 invalid limit. cursor is the opaque unpadded URL-safe base64 a previous page answered; a malformed one answers 400 invalid cursor. Protected bins list with their full metadata in plain view. Key-checked: a wrong API key answers 403 and costs the requesting address a lockout step.")
| .paths["/api/bins/{hash}"].get |= op(
    "inspectBin";
    "Inspect a bin: its object and every file it holds.";
    "Answers 200 with the bin object plus its files — path, size and updated_at — sorted by path, even for a bin whose base is covered by a stored index.html. Needs no API key. A password-protected bin answers 401 bin password required until its password is given as Basic Auth, and that 401 charges nothing.")
| .paths["/api/bins/{hash}"].patch |= op(
    "patchBin";
    "Edit a bin's title, description and password.";
    "A merge with three states per field: present in the body it replaces the stored value — given as null or \"\" it clears it — absent it leaves what the bin holds. {} is a no-op answering the current row; a bodyless request is refused 400. A given password is stored as a bcrypt hash, at most 72 bytes. Key-checked: a wrong API key answers 403 and costs the requesting address a lockout step; a protected bin also needs its current password as Basic Auth for every change, rotating or clearing the password included, and that 401 charges nothing. A refused request changes nothing; an accepted one answers 200 with the bin object as it now stands.")
| .paths["/api/bins/{hash}"].delete |= op(
    "deleteBin";
    "Delete a bin and every file in it.";
    "Carries no request body and answers 204 with an empty body. Key-checked: a wrong API key answers 403 and costs the requesting address a lockout step; a protected bin answers 401 bin password required until Basic Auth carries its current password, and that 401 charges nothing.")
| .paths["/api/health"].get |= op(
    "health";
    "Liveness probe: answers {\"status\":\"ok\"}.";
    "Needs no API key and charges nothing, so it answers even while the requesting address is blocked. Says nothing about bins.")
| .paths["/api/health"].head |= op(
    "headHealth";
    "Liveness probe: answers {\"status\":\"ok\"}.";
    "Needs no API key and charges nothing, so it answers even while the requesting address is blocked. Says nothing about bins.")
| .paths["/api/key"].post |= op(
    "checkApiKey";
    "Check an API key without creating anything.";
    "A working key answers 204 with an empty body and creates nothing. A wrong key answers 403 and costs the requesting address the same lockout step as a wrong key on a write.")
| .paths["/openapi.json"].get |= op(
    "getOpenAPIDocument";
    "Fetch this OpenAPI document.";
    "Answers 200 application/json with this machine-readable contract for the /api surface — the document committed with the server's source, served byte for byte. Needs no API key, touches no store and charges nothing, so it answers even while the requesting address is blocked.")

# Correction 11: the pinned refusal sentences, as examples on the text/plain bodies,
# only where the operation can actually answer that status. One sentence per status for
# 403/404/429/401; an operation with several possible 400 refusals names them all as
# examples, each carrying the exact sentence api.go and handlers.go answer with.
| plain400examples("/api/bins"; "post"; {
    "unreadable-request": { "value": "could not read the request" },
    "unrecognized-field": { "value": "unrecognized field in the request" },
    "title-over-limit": { "value": "title over 200 bytes" },
    "title-control-characters": { "value": "title may not contain control characters" },
    "description-over-limit": { "value": "description over 2000 bytes" },
    "description-control-characters": { "value": "description may not contain control characters" },
    "password-over-limit": { "value": "bin password too long" }
  })
| plain400examples("/api/bins"; "get"; {
    "invalid-limit": { "value": "invalid limit" },
    "invalid-cursor": { "value": "invalid cursor" }
  })
| plain400examples("/api/bins/{hash}"; "patch"; {
    "unreadable-request": { "value": "could not read the request" },
    "unrecognized-field": { "value": "unrecognized field in the request" },
    "title-over-limit": { "value": "title over 200 bytes" },
    "title-control-characters": { "value": "title may not contain control characters" },
    "description-over-limit": { "value": "description over 2000 bytes" },
    "description-control-characters": { "value": "description may not contain control characters" },
    "password-over-limit": { "value": "bin password too long" }
  })
| textexample("/api/bins"; "post"; "403"; "invalid API key")
| textexample("/api/bins"; "get"; "403"; "invalid API key")
| textexample("/api/bins/{hash}"; "patch"; "403"; "invalid API key")
| textexample("/api/bins/{hash}"; "delete"; "403"; "invalid API key")
| textexample("/api/key"; "post"; "403"; "invalid API key")
| textexample("/api/bins"; "post"; "429"; "too many bad API keys")
| textexample("/api/bins"; "get"; "429"; "too many bad API keys")
| textexample("/api/bins/{hash}"; "patch"; "429"; "too many bad API keys")
| textexample("/api/bins/{hash}"; "delete"; "429"; "too many bad API keys")
| textexample("/api/key"; "post"; "429"; "too many bad API keys")
| textexample("/api/bins/{hash}"; "get"; "404"; "unknown bin")
| textexample("/api/bins/{hash}"; "patch"; "404"; "unknown bin")
| textexample("/api/bins/{hash}"; "delete"; "404"; "unknown bin")
| textexample("/api/bins/{hash}"; "get"; "401"; "bin password required")
| textexample("/api/bins/{hash}"; "patch"; "401"; "bin password required")
| textexample("/api/bins/{hash}"; "delete"; "401"; "bin password required")

# Corrections 12 and 13: the two response headers the handlers set through w.Header().Set.
| .paths["/api/bins"].post.responses["201"].headers = {
    "Location": {
      "description": "The new bin's API path: /api/bins/{hash}.",
      "required": true,
      "schema": { "type": "string" }
    }
  }
| .paths["/api/bins"].get.responses["429"].headers = retryAfter
| .paths["/api/bins"].post.responses["429"].headers = retryAfter
| .paths["/api/bins/{hash}"].patch.responses["429"].headers = retryAfter
| .paths["/api/bins/{hash}"].delete.responses["429"].headers = retryAfter
| .paths["/api/key"].post.responses["429"].headers = retryAfter
