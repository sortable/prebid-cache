package endpoints

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/julienschmidt/httprouter"
	"github.com/prebid/prebid-cache/backends"
)

const mockHTTPCacheTTL = 60

func doMockGet(t *testing.T, router *httprouter.Router, id string) *httptest.ResponseRecorder {
	requestRecorder := httptest.NewRecorder()

	body := new(bytes.Buffer)
	getReq, err := http.NewRequest("GET", "/cache"+"?uuid="+id, body)
	if err != nil {
		t.Fatalf("Failed to create a GET request: %v", err)
		return requestRecorder
	}
	router.ServeHTTP(requestRecorder, getReq)
	return requestRecorder
}

func doMockPut(t *testing.T, router *httprouter.Router, content string) (string, *httptest.ResponseRecorder) {
	var parseMockUUID = func(t *testing.T, putResponse string) string {
		var parsed PutResponse
		err := json.Unmarshal([]byte(putResponse), &parsed)
		if err != nil {
			t.Errorf("Response from POST doesn't conform to the expected format: %v", putResponse)
		}
		return parsed.Responses[0].UUID
	}

	rr := httptest.NewRecorder()

	request, err := http.NewRequest("POST", "/cache", strings.NewReader(content))
	if err != nil {
		t.Fatalf("Failed to create a POST request: %v", err)
		return "", rr
	}

	router.ServeHTTP(rr, request)
	uuid := ""
	if rr.Code == http.StatusOK {
		uuid = parseMockUUID(t, rr.Body.String())
	}
	return uuid, rr
}

// expectStored makes a POST request with the given putBody, and then makes sure that expectedGet
// is returned by the GET request for whatever UUID the server chose.
func expectStored(t *testing.T, putBody string, expectedGet string, expectedMimeType string) {
	router := httprouter.New()
	backend := backends.NewMemoryBackend()

	router.POST("/cache", NewPutHandler(backend, 10, true))
	router.GET("/cache", NewGetHandler(backend, true, mockHTTPCacheTTL))

	uuid, putTrace := doMockPut(t, router, putBody)
	if putTrace.Code != http.StatusOK {
		t.Fatalf("Put command failed. Status: %d, Msg: %v", putTrace.Code, putTrace.Body.String())
		return
	}

	getResults := doMockGet(t, router, uuid)
	if getResults.Code != http.StatusOK {
		t.Fatalf("Get command failed with status: %d", getResults.Code)
		return
	}
	if getResults.Body.String() != expectedGet {
		t.Fatalf("Expected GET response %v to equal %v", getResults.Body.String(), expectedGet)
		return
	}
	if getResults.Header().Get("Content-Type") != expectedMimeType {
		t.Fatalf("Expected GET response Content-Type %v to equal %v", getResults.Header().Get("Content-Type"), expectedMimeType)
	}
}

// expectFailedPut makes a POST request with the given request body, and fails unless the server
// responds with a 400
func expectFailedPut(t *testing.T, requestBody string) {
	backend := backends.NewMemoryBackend()
	router := httprouter.New()
	router.POST("/cache", NewPutHandler(backend, 10, true))

	_, putTrace := doMockPut(t, router, requestBody)
	if putTrace.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 response. Got: %d, Msg: %v", putTrace.Code, putTrace.Body.String())
		return
	}
}

func TestJSONString(t *testing.T) {
	expectStored(
		t,
		"{\"puts\":[{\"type\":\"json\",\"value\":\"plain text\"}]}",
		"\"plain text\"",
		"application/json")
}

func TestEscapedString(t *testing.T) {
	expectStored(
		t,
		"{\"puts\":[{\"type\":\"json\",\"value\":\"esca\\\"ped\"}]}",
		"\"esca\\\"ped\"",
		"application/json")
}

func TestUnescapedString(t *testing.T) {
	expectFailedPut(t, "{\"puts\":[{\"type\":\"json\",\"value\":\"badly-esca\"ped\"}]}")
}

func TestNumber(t *testing.T) {
	expectStored(t, "{\"puts\":[{\"type\":\"json\",\"value\":5}]}", "5", "application/json")
}

func TestObject(t *testing.T) {
	expectStored(
		t,
		"{\"puts\":[{\"type\":\"json\",\"value\":{\"custom_key\":\"foo\"}}]}",
		"{\"custom_key\":\"foo\"}",
		"application/json")
}

func TestNull(t *testing.T) {
	expectStored(t, "{\"puts\":[{\"type\":\"json\",\"value\":null}]}", "null", "application/json")
}

func TestBoolean(t *testing.T) {
	expectStored(t, "{\"puts\":[{\"type\":\"json\",\"value\":true}]}", "true", "application/json")
}

func TestExtraProperty(t *testing.T) {
	expectStored(
		t,
		"{\"puts\":[{\"type\":\"json\",\"value\":null,\"irrelevant\":\"foo\"}]}",
		"null",
		"application/json")
}

func TestInvalidJSON(t *testing.T) {
	expectFailedPut(t, "{\"puts\":[{\"type\":\"json\",\"value\":malformed}]}")
}

func TestMissingProperty(t *testing.T) {
	expectFailedPut(t, "{\"puts\":[{\"type\":\"json\",\"unrecognized\":true}]}")
}

func TestMixedValidityPuts(t *testing.T) {
	expectFailedPut(t, "{\"puts\":[{\"type\":\"json\",\"value\":true}, {\"type\":\"json\",\"unrecognized\":true}]}")
}

func TestXMLString(t *testing.T) {
	expectStored(t, "{\"puts\":[{\"type\":\"xml\",\"value\":\"<tag></tag>\"}]}", "<tag></tag>", "application/xml")
}

func TestCrossScriptEscaping(t *testing.T) {
	expectStored(t, "{\"puts\":[{\"type\":\"xml\",\"value\":\"<tag>esc\\\"aped</tag>\"}]}", "<tag>esc\"aped</tag>", "application/xml")
}

func TestXMLOther(t *testing.T) {
	expectFailedPut(t, "{\"puts\":[{\"type\":\"xml\",\"value\":5}]}")
}

func TestGetInvalidUUIDs(t *testing.T) {
	backend := backends.NewMemoryBackend()
	router := httprouter.New()
	router.GET("/cache", NewGetHandler(backend, false, mockHTTPCacheTTL))

	getResults := doMockGet(t, router, "fdd9405b-ef2b-46da-a55a-2f526d338e16")
	if getResults.Code != http.StatusNotFound {
		t.Fatalf("Expected GET to return 404 on unrecognized ID. Got: %d", getResults.Code)
		return
	}

	getResults = doMockGet(t, router, "abc")
	if getResults.Code != http.StatusNotFound {
		t.Fatalf("Expected GET to return 404 on unrecognized ID. Got: %d", getResults.Code)
		return
	}
}

func TestReadinessCheck(t *testing.T) {
	requestRecorder := httptest.NewRecorder()

	router := httprouter.New()
	router.GET("/status", Status)
	req, _ := http.NewRequest("GET", "/status", new(bytes.Buffer))
	router.ServeHTTP(requestRecorder, req)

	if requestRecorder.Code != http.StatusNoContent {
		t.Errorf("/status endpoint should always return a 204. Got %d", requestRecorder.Code)
	}
}
