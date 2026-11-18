package main

import (
	"log"
	"net/http"

	"github.com/yookoala/sandbin/internal/version"
)

// apiIndexPathOrder is the fixed enumeration the index publishes its endpoints in — the order
// the format of docs/decisions/0031-api-index-endpoint.md shows them. It and the descriptions
// map are pinned against the route table by TestAPIIndexPathOrderEqualsTheTable and
// TestAPIIndexDescriptionsCoverTheTableOneForOne: a route that joins the table without joining
// both lists fails the suite.
var apiIndexPathOrder = []string{"/api/key", "/api/health", "/api/bins", "/api/bins/{hash}"}

// apiIndexDescriptions pins one sentence per published pattern, keyed by the exact pattern
// strings, copied verbatim from the decision doc's index format. The two index patterns are
// the single sanctioned gap — the index does not describe itself — and the coverage pin holds
// every other pattern to exactly one entry. A rewording here must consciously move the golden
// body in TestAPIIndexBodyIsThePublishedSurface along with it.
var apiIndexDescriptions = map[string]string{
	apiPatternKey:        "Checks an API key without touching a bin: a good key answers 204, a wrong key costs the requesting address a lockout step.",
	apiPatternHealthGet:  "Unauthenticated liveness probe; answers 200 even while an address is blocked.",
	apiPatternHealthHead: "The same probe with no body.",
	apiPatternBinsGet:    "Lists bins with cursor paging; a wrong key costs the address a lockout step.",
	apiPatternBinsPost:   "Creates a bin from a JSON body; 201 answers with the bin object and a Location header.",
	apiPatternBinGet:     "Inspects one bin; a password-protected bin needs the bin password.",
	apiPatternBinPatch:   "Updates title, description, or password; a wrong key costs the address a lockout step.",
	apiPatternBinDelete:  "Deletes the bin; the request takes no body.",
	apiPatternOpenAPI:    "The OpenAPI 3.1 document describing this whole surface.",
}

// apiIndexMethod is one published method on one endpoint, with the decision doc's sentence for
// it.
type apiIndexMethod struct {
	Method      string `json:"method"`
	Description string `json:"description"`
}

// apiIndexEndpoint is one row of the index: the URL as the visitor can fetch it — the literal
// {hash} spelling for the per-bin row, same as the OpenAPI document uses — and every method the
// route table allows on that path, in the table's canonical order.
type apiIndexEndpoint struct {
	URL     string           `json:"url"`
	Methods []apiIndexMethod `json:"methods"`
}

// apiIndexLink is a pointer to a document: its full URL and one sentence about it.
type apiIndexLink struct {
	URL         string `json:"url"`
	Description string `json:"description"`
}

// apiIndexDoc is the GET /api answer: the stamped version, the endpoint rows enumerated from
// the route table, and the openapi sibling pointing at the machine-readable contract.
type apiIndexDoc struct {
	Version   string             `json:"version"`
	Endpoints []apiIndexEndpoint `json:"endpoints"`
	OpenAPI   apiIndexLink       `json:"openapi"`
}

// apiIndex answers GET /api (and HEAD, registered as its own spelling): the live map of the
// published surface, built from the same route table the dispatch runs on, so the map can
// never disagree with the server. Every URL is frontBase(r) plus the table's own path — host
// and scheme from the visitor's request context, nothing hardcoded — and the index does not
// list itself: it IS /api. It needs no API key, touches no store and charges nothing, so it
// answers even while the requesting address is blocked.
func (s *server) apiIndex(w http.ResponseWriter, r *http.Request) {
	base := s.frontBase(r)

	doc := apiIndexDoc{
		Version:   version.Value,
		Endpoints: make([]apiIndexEndpoint, 0, len(apiIndexPathOrder)),
	}
	for _, path := range apiIndexPathOrder {
		allowed := s.apiMethods[path]
		endpoint := apiIndexEndpoint{
			URL:     base + path,
			Methods: make([]apiIndexMethod, 0, len(allowed)),
		}
		for _, method := range allowed {
			endpoint.Methods = append(endpoint.Methods, apiIndexMethod{
				Method:      method,
				Description: apiIndexDescriptions[method+" "+path],
			})
		}
		doc.Endpoints = append(doc.Endpoints, endpoint)
	}
	doc.OpenAPI = apiIndexLink{
		URL:         base + "/openapi.json",
		Description: apiIndexDescriptions[apiPatternOpenAPI],
	}

	if err := writeJSON(w, http.StatusOK, doc); err != nil {
		log.Printf("encode the API index: %v", err)
		http.Error(w, "could not answer the API index", http.StatusInternalServerError)
	}
}
