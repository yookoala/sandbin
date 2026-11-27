# Post-generation corrections for cmd/sandbin/openapi.json (plan 0030, ruling R4).
# `make openapi` pipes apispec v0.5.11 through this filter because the generator
# verifiably cannot express these eighteen things itself:
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
# 11. Spell every documented refusal as the JSON error envelope: the named component
#     `apiErrorBody` (status, code, message — all required, nothing else) and, on each
#     documented non-2xx response, `application/json` carrying it with example values for
#     the pinned sentences (api.go and handlers.go) that operation can actually answer.
#     apispec records the status of a writeAPIError call but no body, since the call hands
#     the message as a string argument and not a type the scan can read.
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
# 16. Strip the body schema HEAD /api answers with. apiIndex joins apispec.yaml's writeJSON
#     pattern, so the generator now sees the marshaled index body: GET /api's 200 is generated as a
#     $ref to the apiIndexDoc component apispec derived from the DTO, and needs no correction. The
#     one left is HEAD: both index registrations bind the same handler, and the scan attributes the
#     same writeJSON call to the HEAD 200 — but HEAD answers no body (docs/decisions/0031-api-index-
#     endpoint.md), and apispec cannot know a handler's marshaled body is not written on one of the
#     registrations it shares.
# 17. Name the two index operations in the correction 10 style: client-facing summaries — the
#     GET's names the JSON body, the HEAD's says it answers headers only — descriptions, and
#     the operationIds apiIndex and headAPIIndex.
# 18. Write `additionalProperties: false` into the four apiIndex components (apiIndexDoc,
#     apiIndexEndpoint, apiIndexMethod, apiIndexLink): apispec emits no such flag for a generated
#     component, and the config cannot demand it — a `schema: additionalProperties` entry is read
#     with no effect on v0.5.11 (verified), and the externalType Schema type rejects the boolean
#     outright (correction 14's family of reasons). The DTOs the handler marshals are closed, so
#     the schemas that describe them are closed too.

def op($id; $summary; $description):
  .operationId = $id | .summary = $summary | .description = $description;
def envelopeexamples($code; $map):
  $map | map_values({ "value": { "status": "error", "code": $code, "message": . } });
def jsonrefusals($path; $method; $status; $code; $map):
  .paths[$path][$method].responses[$status].content =
    ((.paths[$path][$method].responses[$status].content // {})
     | del(."text/plain; charset=utf-8")
     | .["application/json"] = {
         "schema": { "$ref": "#/components/schemas/apiErrorBody" },
         "examples": envelopeexamples($code; $map)
       });
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
# Correction 16: apispec generates GET /api's 200 with its $ref to apiIndexDoc untouched; only
# HEAD's wrongly attached body schema is stripped.
| del(.paths["/api"].head.responses["200"].content)
| .components.schemas.archiveAnswer = {
    "type": "object",
    "description": "archiveAnswer is the JSON a successful archive upload answers: its own status first, then the store's created and replaced counts for the extracted entries.",
    "required": ["status", "created", "replaced"],
    "properties": {
      "status": {
        "type": "string"
      },
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
# Correction 18: the four apiIndex components are as closed as the DTOs they describe.
| .components.schemas.apiIndexDoc.additionalProperties = false
| .components.schemas.apiIndexEndpoint.additionalProperties = false
| .components.schemas.apiIndexMethod.additionalProperties = false
| .components.schemas.apiIndexLink.additionalProperties = false
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
    "Index of this API — headers only.";
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
    "Carries no request body and answers 200 with {\"status\":\"ok\"}. Key-checked: a wrong API key answers 403 and costs the requesting address a lockout step; a protected bin answers 401 bin password required until Basic Auth carries its current password, and that 401 charges nothing.")
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
    "A working key answers 200 with {\"status\":\"ok\"} and creates nothing. A wrong key answers 403 and costs the requesting address the same lockout step as a wrong key on a write.")
| .paths["/openapi.json"].get |= op(
    "getOpenAPIDocument";
    "Fetch this OpenAPI document.";
    "Answers 200 application/json with this machine-readable contract for the /api surface — the document committed with the server's source, served byte for byte. Needs no API key, touches no store and charges nothing, so it answers even while the requesting address is blocked.")

# Correction 11: the JSON error envelope, named as a component and carried as application/json
# on every documented refusal. The scan reads a status off writeAPIError but no body — the call
# has no value argument to infer a type from — so only this filter can say what an API refusal
# answers: {"status":"error","code":<the status>,"message":<the fixed sentence>}. Each refusal
# names, as examples, the exact sentences the handler behind that operation answers with.
| .components.schemas.apiErrorBody = {
    "description": "A refusal this API answers: the status it answered, said twice (once as the word error, once as the number), and the fixed sentence naming what went wrong. Every /api refusal answers this shape.",
    "type": "object",
    "properties": {
      "status": {
        "description": "Always the word error.",
        "type": "string"
      },
      "code": {
        "description": "The HTTP status answered, the same number as the response's status code.",
        "type": "integer"
      },
      "message": {
        "description": "The fixed sentence naming the refusal.",
        "type": "string"
      }
    },
    "required": [
      "status",
      "code",
      "message"
    ],
    "additionalProperties": false
  }
| jsonrefusals("/api/bins"; "get"; "400"; 400; {
    "invalid-limit": "invalid limit",
    "invalid-cursor": "invalid cursor"
  })
| jsonrefusals("/api/bins"; "get"; "403"; 403; { "invalid-api-key": "invalid API key" })
| jsonrefusals("/api/bins"; "get"; "429"; 429; { "too-many-bad-keys": "too many bad API keys" })
| jsonrefusals("/api/bins"; "get"; "500"; 500; { "could-not-list-bins": "could not list bins" })
| jsonrefusals("/api/bins"; "post"; "400"; 400; {
    "could-not-read-the-request": "could not read the request",
    "unrecognized-field-in-the-request": "unrecognized field in the request",
    "title-over-limit": "title over 200 bytes",
    "title-control-characters": "title may not contain control characters",
    "description-over-limit": "description over 2000 bytes",
    "description-control-characters": "description may not contain control characters",
    "password-over-limit": "bin password too long"
  })
| jsonrefusals("/api/bins"; "post"; "403"; 403; { "invalid-api-key": "invalid API key" })
| jsonrefusals("/api/bins"; "post"; "429"; 429; { "too-many-bad-keys": "too many bad API keys" })
| jsonrefusals("/api/bins"; "post"; "500"; 500; {
    "could-not-create-bin": "could not create bin",
    "could-not-set-the-bin-password": "could not set the bin password",
    "could-not-answer-the-bin": "could not answer the bin"
  })
| jsonrefusals("/api/bins/{hash}"; "get"; "401"; 401; { "bin-password-required": "bin password required" })
| jsonrefusals("/api/bins/{hash}"; "get"; "404"; 404; { "bin-not-found": "bin not found" })
| jsonrefusals("/api/bins/{hash}"; "get"; "500"; 500; {
    "could-not-list-bin": "could not list bin",
    "could-not-answer-the-bin": "could not answer the bin"
  })
| jsonrefusals("/api/bins/{hash}"; "delete"; "401"; 401; { "bin-password-required": "bin password required" })
| jsonrefusals("/api/bins/{hash}"; "delete"; "403"; 403; { "invalid-api-key": "invalid API key" })
| jsonrefusals("/api/bins/{hash}"; "delete"; "404"; 404; { "bin-not-found": "bin not found" })
| jsonrefusals("/api/bins/{hash}"; "delete"; "429"; 429; { "too-many-bad-keys": "too many bad API keys" })
| jsonrefusals("/api/bins/{hash}"; "delete"; "500"; 500; { "could-not-delete-bin": "could not delete bin" })
| jsonrefusals("/api/bins/{hash}"; "patch"; "400"; 400; {
    "could-not-read-the-request": "could not read the request",
    "unrecognized-field-in-the-request": "unrecognized field in the request",
    "title-over-limit": "title over 200 bytes",
    "title-control-characters": "title may not contain control characters",
    "description-over-limit": "description over 2000 bytes",
    "description-control-characters": "description may not contain control characters",
    "password-over-limit": "bin password too long"
  })
| jsonrefusals("/api/bins/{hash}"; "patch"; "401"; 401; { "bin-password-required": "bin password required" })
| jsonrefusals("/api/bins/{hash}"; "patch"; "403"; 403; { "invalid-api-key": "invalid API key" })
| jsonrefusals("/api/bins/{hash}"; "patch"; "404"; 404; { "bin-not-found": "bin not found" })
| jsonrefusals("/api/bins/{hash}"; "patch"; "429"; 429; { "too-many-bad-keys": "too many bad API keys" })
| jsonrefusals("/api/bins/{hash}"; "patch"; "500"; 500; {
    "could-not-set-the-bin-password": "could not set the bin password",
    "could-not-update-bin": "could not update bin",
    "could-not-answer-the-bin": "could not answer the bin"
  })
| jsonrefusals("/api"; "get"; "500"; 500; { "could-not-answer-the-api-index": "could not answer the API index" })
| jsonrefusals("/api"; "head"; "500"; 500; { "could-not-answer-the-api-index": "could not answer the API index" })
| jsonrefusals("/api/health"; "get"; "500"; 500; { "could-not-answer-health": "could not answer health" })
| jsonrefusals("/api/health"; "head"; "500"; 500; { "could-not-answer-health": "could not answer health" })
| jsonrefusals("/api/key"; "post"; "403"; 403; { "invalid-api-key": "invalid API key" })
| jsonrefusals("/api/key"; "post"; "429"; 429; { "too-many-bad-keys": "too many bad API keys" })

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
