package cors

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/stretchr/testify/assert"
)

func newTestRouter(config Config) *chi.Mux {
	router := chi.NewRouter()
	if config.Enabled == true {
		router.Use(New(config))
	}
	router.Get("/", func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
		writer.Write([]byte("get"))
	})
	router.Post("/", func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
		writer.Write([]byte("post"))
	})
	router.Patch("/", func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
		writer.Write([]byte("patch"))
	})
	return router
}

func performRequest(r http.Handler, method, origin string) *httptest.ResponseRecorder {
	return performRequestWithHeaders(r, method, origin, http.Header{})
}

func performRequestWithHeaders(r http.Handler, method, origin string, header http.Header) *httptest.ResponseRecorder {
	req, _ := http.NewRequestWithContext(context.Background(), method, "/", nil)
	// From go/net/http/request.go:
	// For incoming requests, the Host header is promoted to the
	// Request.Host field and removed from the Header map.
	req.Host = header.Get("Host")
	header.Del("Host")
	if len(origin) > 0 {
		header.Set("Origin", origin)
	}
	req.Header = header
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestConfigAddAllow(t *testing.T) {
	config := Config{
		Enabled: true,
	}
	config.AddAllowMethods("POST")
	config.AddAllowMethods("GET", "PUT")
	config.AddExposeHeaders()

	config.AddAllowHeaders("Some", " cool")
	config.AddAllowHeaders("header")
	config.AddExposeHeaders()

	config.AddExposeHeaders()
	config.AddExposeHeaders("exposed", "header")
	config.AddExposeHeaders("hey")

	assert.Equal(t, config.AllowMethods, []string{"POST", "GET", "PUT"})
	assert.Equal(t, config.AllowHeaders, []string{"Some", " cool", "header"})
	assert.Equal(t, config.ExposeHeaders, []string{"exposed", "header", "hey"})
}

func TestBadConfig(t *testing.T) {
	assert.Panics(t, func() {
		New(Config{
			Enabled: true,
		})(nil)
	})
	assert.Panics(t, func() {
		New(Config{
			Enabled:         true,
			AllowAllOrigins: true,
			AllowOrigins:    []string{"http://google.com"},
		})(nil)
	})
	assert.Panics(t, func() {
		New(Config{
			Enabled:         true,
			AllowAllOrigins: true,
			AllowOriginFunc: func(origin string) bool { return false },
		})(nil)
	})
	assert.Panics(t, func() {
		New(Config{
			Enabled:      true,
			AllowOrigins: []string{"google.com"},
		})(nil)
	})
}

func TestNormalize(t *testing.T) {
	values := normalize([]string{
		"http-Access ", "Post", "POST", " poSt  ",
		"HTTP-Access", "",
	})
	assert.Equal(t, values, []string{"http-access", "post", ""})

	values = normalize(nil)
	assert.Nil(t, values)

	values = normalize([]string{})
	assert.Equal(t, values, []string{})
}

func TestConvert(t *testing.T) {
	methods := []string{"Get", "GET", "get"}
	headers := []string{"X-CSRF-TOKEN", "X-CSRF-Token", "x-csrf-token"}

	assert.Equal(t, []string{"GET", "GET", "GET"}, convert(methods, strings.ToUpper))
	assert.Equal(t, []string{"X-Csrf-Token", "X-Csrf-Token", "X-Csrf-Token"}, convert(headers, http.CanonicalHeaderKey))
}

func TestGenerateNormalHeaders_AllowAllOrigins(t *testing.T) {
	header := generateNormalHeaders(Config{
		Enabled:         true,
		AllowAllOrigins: false,
	})
	assert.Equal(t, header.Get("Access-Control-Allow-Origin"), "")
	assert.Equal(t, header.Get("Vary"), "Origin")
	assert.Len(t, header, 1)

	header = generateNormalHeaders(Config{
		Enabled:         true,
		AllowAllOrigins: true,
	})
	assert.Equal(t, header.Get("Access-Control-Allow-Origin"), "*")
	assert.Equal(t, header.Get("Vary"), "")
	assert.Len(t, header, 1)
}

func TestGenerateNormalHeaders_AllowCredentials(t *testing.T) {
	header := generateNormalHeaders(Config{
		Enabled:          true,
		AllowCredentials: true,
	})
	assert.Equal(t, header.Get("Access-Control-Allow-Credentials"), "true")
	assert.Equal(t, header.Get("Vary"), "Origin")
	assert.Len(t, header, 2)
}

func TestGenerateNormalHeaders_ExposedHeaders(t *testing.T) {
	header := generateNormalHeaders(Config{
		Enabled:       true,
		ExposeHeaders: []string{"X-user", "xPassword"},
	})
	assert.Equal(t, header.Get("Access-Control-Expose-Headers"), "X-User,Xpassword")
	assert.Equal(t, header.Get("Vary"), "Origin")
	assert.Len(t, header, 2)
}

func TestGeneratePreflightHeaders(t *testing.T) {
	header := generatePreflightHeaders(Config{
		Enabled:         true,
		AllowAllOrigins: false,
	})
	assert.Equal(t, header.Get("Access-Control-Allow-Origin"), "")
	assert.Equal(t, header.Get("Vary"), "Origin")
	assert.Len(t, header, 1)

	header = generateNormalHeaders(Config{
		Enabled:         true,
		AllowAllOrigins: true,
	})
	assert.Equal(t, header.Get("Access-Control-Allow-Origin"), "*")
	assert.Equal(t, header.Get("Vary"), "")
	assert.Len(t, header, 1)
}

func TestGeneratePreflightHeaders_AllowCredentials(t *testing.T) {
	header := generatePreflightHeaders(Config{
		Enabled:          true,
		AllowCredentials: true,
	})
	assert.Equal(t, header.Get("Access-Control-Allow-Credentials"), "true")
	assert.Equal(t, header.Get("Vary"), "Origin")
	assert.Len(t, header, 2)
}

func TestGeneratePreflightHeaders_AllowMethods(t *testing.T) {
	header := generatePreflightHeaders(Config{
		Enabled:      true,
		AllowMethods: []string{"GET ", "post", "PUT", " put  "},
	})
	assert.Equal(t, header.Get("Access-Control-Allow-Methods"), "GET,POST,PUT")
	assert.Equal(t, header.Get("Vary"), "Origin")
	assert.Len(t, header, 2)
}

func TestGeneratePreflightHeaders_AllowHeaders(t *testing.T) {
	header := generatePreflightHeaders(Config{
		Enabled:      true,
		AllowHeaders: []string{"X-user", "Content-Type"},
	})
	assert.Equal(t, header.Get("Access-Control-Allow-Headers"), "X-User,Content-Type")
	assert.Equal(t, header.Get("Vary"), "Origin")
	assert.Len(t, header, 2)
}

func TestGeneratePreflightHeaders_MaxAge(t *testing.T) {
	header := generatePreflightHeaders(Config{
		Enabled: true,
		MaxAge:  12 * time.Hour,
	})
	assert.Equal(t, header.Get("Access-Control-Max-Age"), "43200") // 12*60*60
	assert.Equal(t, header.Get("Vary"), "Origin")
	assert.Len(t, header, 2)
}

func TestExtremeLengthOriginKillswitch(t *testing.T) {
	cors := newCors(nil, Config{
		Enabled:      true,
		AllowOrigins: []string{"https://*.google.com"},
	})

	shortSubdomain := strings.Repeat("a", 10)
	longSubdomain := strings.Repeat("a", 500)
	tooLongSubdomain := strings.Repeat("a", 4096)

	assert.True(t, cors.validateOrigin(fmt.Sprintf("https://%s.google.com", shortSubdomain)))
	assert.True(t, cors.validateOrigin(fmt.Sprintf("https://%s.google.com", longSubdomain)))
	assert.False(t, cors.validateOrigin(fmt.Sprintf("https://%s.google.com", tooLongSubdomain)))

	// Should not affect strict origins
	cors = newCors(nil, Config{
		Enabled:      true,
		AllowOrigins: []string{fmt.Sprintf("https://%s.google.com", tooLongSubdomain)},
	})

	assert.True(t, cors.validateOrigin(fmt.Sprintf("https://%s.google.com", tooLongSubdomain)))
}

func TestValidateOrigin(t *testing.T) {
	cors := newCors(nil, Config{
		Enabled:         true,
		AllowAllOrigins: true,
	})
	assert.True(t, cors.validateOrigin("http://google.com"))
	assert.True(t, cors.validateOrigin("https://google.com"))
	assert.True(t, cors.validateOrigin("example.com"))
	assert.True(t, cors.validateOrigin("chrome-extension://random-extension-id"))

	cors = newCors(nil, Config{
		Enabled:      true,
		AllowOrigins: []string{"https://google.com", "https://github.com"},
		AllowOriginFunc: func(origin string) bool {
			return (origin == "http://news.ycombinator.com")
		},
		AllowBrowserExtensions: true,
	})
	assert.False(t, cors.validateOrigin("http://google.com"))
	assert.True(t, cors.validateOrigin("https://google.com"))
	assert.True(t, cors.validateOrigin("https://github.com"))
	assert.True(t, cors.validateOrigin("http://news.ycombinator.com"))
	assert.False(t, cors.validateOrigin("http://example.com"))
	assert.False(t, cors.validateOrigin("google.com"))
	assert.False(t, cors.validateOrigin("chrome-extension://random-extension-id"))

	cors = newCors(nil, Config{
		Enabled:      true,
		AllowOrigins: []string{"https://google.com", "https://github.com"},
	})
	assert.False(t, cors.validateOrigin("chrome-extension://random-extension-id"))
	assert.False(t, cors.validateOrigin("file://some-dangerous-file.js"))
	assert.False(t, cors.validateOrigin("wss://socket-connection"))

	cors = newCors(nil, Config{
		Enabled: true,
		AllowOrigins: []string{
			"chrome-extension://*",
			"safari-extension://my-extension-*-app",
			"*.some-domain.com",
		},
		AllowBrowserExtensions: true,
	})
	assert.True(t, cors.validateOrigin("chrome-extension://random-extension-id"))
	assert.True(t, cors.validateOrigin("chrome-extension://another-one"))
	assert.True(t, cors.validateOrigin("safari-extension://my-extension-one-app"))
	assert.True(t, cors.validateOrigin("safari-extension://my-extension-two-app"))
	assert.False(t, cors.validateOrigin("moz-extension://ext-id-we-not-allow"))
	assert.True(t, cors.validateOrigin("http://api.some-domain.com"))
	assert.False(t, cors.validateOrigin("http://api.another-domain.com"))

	cors = newCors(nil, Config{
		Enabled:         true,
		AllowOrigins:    []string{"file://safe-file.js", "wss://some-session-layer-connection"},
		AllowFiles:      true,
		AllowWebSockets: true,
	})
	assert.True(t, cors.validateOrigin("file://safe-file.js"))
	assert.False(t, cors.validateOrigin("file://some-dangerous-file.js"))
	assert.True(t, cors.validateOrigin("wss://some-session-layer-connection"))
	assert.False(t, cors.validateOrigin("ws://not-what-we-expected"))

	cors = newCors(nil, Config{
		Enabled:      true,
		AllowOrigins: []string{"*"},
	})
	assert.True(t, cors.validateOrigin("http://google.com"))
	assert.True(t, cors.validateOrigin("https://google.com"))
	assert.True(t, cors.validateOrigin("example.com"))
	assert.True(t, cors.validateOrigin("chrome-extension://random-extension-id"))

	// Wildcards
	cors = newCors(nil, Config{
		Enabled: true,
		AllowOrigins: []string{
			"https://*.wgexample.com",
			"https://wgexample.com",
			"https://*.wgexample.io:*",
			"https://*.wgexample.org",
			"https://*.d2grknavcceso7.amplifyapp.com",
		},
	})
	// Matching cases for "*.wgexample.com" wildcard
	assert.True(t, cors.validateOrigin("https://subdomain.wgexample.com"))
	assert.True(t, cors.validateOrigin("https://another.subdomain.wgexample.com"))
	assert.True(t, cors.validateOrigin("https://unauthorized.wgexample.com"))

	assert.True(t, cors.validateOrigin("https://wgexample.com"))

	assert.True(t, cors.validateOrigin("https://subdomain.wgexample.io:443"))
	assert.True(t, cors.validateOrigin("https://api.wgexample.io:8080"))

	assert.True(t, cors.validateOrigin("https://project.wgexample.org"))
	assert.True(t, cors.validateOrigin("https://beta.wgexample.org"))

	assert.True(t, cors.validateOrigin("https://service.d2grknavcceso7.amplifyapp.com"))
	assert.True(t, cors.validateOrigin("https://prod.d2grknavcceso7.amplifyapp.com"))
	assert.True(t, cors.validateOrigin("https://otherdomain.second.d2grknavcceso7.amplifyapp.com"))

	assert.False(t, cors.validateOrigin("https://random.com"))
	assert.False(t, cors.validateOrigin("https://wgexample.io"))
	assert.False(t, cors.validateOrigin("https://wgexample.org"))
	assert.False(t, cors.validateOrigin("http://subdomain.wgexample.com")) // Different scheme (http instead of https)
}

func TestPassesAllowOrigins(t *testing.T) {
	router := newTestRouter(Config{
		Enabled:          true,
		AllowOrigins:     []string{"http://google.com"},
		AllowMethods:     []string{" GeT ", "get", "post", "PUT  ", "Head", "POST"},
		AllowHeaders:     []string{"Content-type", "timeStamp "},
		ExposeHeaders:    []string{"Data", "x-User"},
		AllowCredentials: false,
		MaxAge:           12 * time.Hour,
		AllowOriginFunc: func(origin string) bool {
			return origin == "http://github.com"
		},
	})

	// no CORS request, origin == ""
	w := performRequest(router, "GET", "")
	assert.Equal(t, "get", w.Body.String())
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
	assert.Empty(t, w.Header().Get("Access-Control-Expose-Headers"))

	// no CORS request, origin == host
	h := http.Header{}
	h.Set("Host", "facebook.com")
	w = performRequestWithHeaders(router, "GET", "http://facebook.com", h)
	assert.Equal(t, "get", w.Body.String())
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
	assert.Empty(t, w.Header().Get("Access-Control-Expose-Headers"))

	// allowed CORS request
	w = performRequest(router, "GET", "http://google.com")
	assert.Equal(t, "get", w.Body.String())
	assert.Equal(t, "http://google.com", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "", w.Header().Get("Access-Control-Allow-Credentials"))
	assert.Equal(t, "Data,X-User", w.Header().Get("Access-Control-Expose-Headers"))

	w = performRequest(router, "GET", "http://github.com")
	assert.Equal(t, "get", w.Body.String())
	assert.Equal(t, "http://github.com", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "", w.Header().Get("Access-Control-Allow-Credentials"))
	assert.Equal(t, "Data,X-User", w.Header().Get("Access-Control-Expose-Headers"))

	// deny CORS request
	w = performRequest(router, "GET", "https://google.com")
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
	assert.Empty(t, w.Header().Get("Access-Control-Expose-Headers"))

	// allowed CORS preflight request
	w = performRequest(router, "OPTIONS", "http://github.com")
	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "http://github.com", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "", w.Header().Get("Access-Control-Allow-Credentials"))
	assert.Equal(t, "GET,POST,PUT,HEAD", w.Header().Get("Access-Control-Allow-Methods"))
	assert.Equal(t, "Content-Type,Timestamp", w.Header().Get("Access-Control-Allow-Headers"))
	assert.Equal(t, "43200", w.Header().Get("Access-Control-Max-Age"))

	// deny CORS preflight request
	w = performRequest(router, "OPTIONS", "http://example.com")
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Methods"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Headers"))
	assert.Empty(t, w.Header().Get("Access-Control-Max-Age"))
}

func TestPassesAllowAllOrigins(t *testing.T) {
	router := newTestRouter(Config{
		Enabled:          true,
		AllowAllOrigins:  true,
		AllowMethods:     []string{" Patch ", "get", "post", "POST"},
		AllowHeaders:     []string{"Content-type", "  testheader "},
		ExposeHeaders:    []string{"Data2", "x-User2"},
		AllowCredentials: false,
		MaxAge:           10 * time.Hour,
	})

	// no CORS request, origin == ""
	w := performRequest(router, "GET", "")
	assert.Equal(t, "get", w.Body.String())
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
	assert.Empty(t, w.Header().Get("Access-Control-Expose-Headers"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))

	// allowed CORS request
	w = performRequest(router, "POST", "example.com")
	assert.Equal(t, "post", w.Body.String())
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "Data2,X-User2", w.Header().Get("Access-Control-Expose-Headers"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))

	// allowed CORS prefligh request
	w = performRequest(router, "OPTIONS", "https://facebook.com")
	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "PATCH,GET,POST", w.Header().Get("Access-Control-Allow-Methods"))
	assert.Equal(t, "Content-Type,Testheader", w.Header().Get("Access-Control-Allow-Headers"))
	assert.Equal(t, "36000", w.Header().Get("Access-Control-Max-Age"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
}

func TestWildcard(t *testing.T) {
	router := newTestRouter(Config{
		Enabled:      true,
		AllowOrigins: []string{"https://*.github.com", "https://api.*", "http://*", "https://facebook.com", "*.golang.org"},
		AllowMethods: []string{"GET"},
	})

	w := performRequest(router, "GET", "https://gist.github.com")
	assert.Equal(t, 200, w.Code)

	w = performRequest(router, "GET", "https://api.github.com/v1/users")
	assert.Equal(t, 200, w.Code)

	w = performRequest(router, "GET", "https://giphy.com/")
	assert.Equal(t, 403, w.Code)

	w = performRequest(router, "GET", "http://hard-to-find-http-example.com")
	assert.Equal(t, 200, w.Code)

	w = performRequest(router, "GET", "https://facebook.com")
	assert.Equal(t, 200, w.Code)

	w = performRequest(router, "GET", "https://something.golang.org")
	assert.Equal(t, 200, w.Code)

	w = performRequest(router, "GET", "https://something.go.org")
	assert.Equal(t, 403, w.Code)

	router = newTestRouter(Config{
		Enabled:      true,
		AllowOrigins: []string{"https://github.com", "https://facebook.com"},
		AllowMethods: []string{"GET"},
	})

	w = performRequest(router, "GET", "https://gist.github.com")
	assert.Equal(t, 403, w.Code)

	w = performRequest(router, "GET", "https://github.com")
	assert.Equal(t, 200, w.Code)
}

func TestComplexWildcards(t *testing.T) {
	router := newTestRouter(Config{
		Enabled: true,
		AllowOrigins: []string{
			"https://*.wgexample.com",
			"https://wgexample.com",
			"https://*.wgexample.io:*",
			"https://*.wgexample.org",
			"https://*.d2grknavcceso7.amplifyapp.com",
			"https://*.example.*.*.com", // multiple sequential wildcards
			"https://*.*.*.*.com",
		},
		AllowMethods: []string{"GET"},
	})

	type testCases struct {
		origin       string
		expectedCode int
	}

	testCasesList := []testCases{
		{"https://subdomain.wgexample.com", 200},
		{"https://another.subdomain.wgexample.com", 200},
		{"https://another.wgexample.subdomain.wgexample.com", 200},
		// Specfically test backtracking, to make sure not only greedy solution
		{"https://another.wgexample.subdomain.with.wgexample.wgexample.com", 200},
		// Complex backtracking which should fail in the end
		{"https://another.wgexample.subdomain.with.wgexample.wgexample.io", 403},
		{"https://wgexample.com", 200},
		{"https://subdomain.wgexample.io:443", 200},
		{"https://api.wgexample.io:8080", 200},
		{"https://project.wgexample.org", 200},
		{"https://beta.wgexample.org", 200},
		{"https://service.d2grknavcceso7.amplifyapp.com", 200},
		{"https://prod.d2grknavcceso7.amplifyapp.com", 200},
		{"https://otherdomain.second.d2grknavcceso7.amplifyapp.com", 200},
		{"https://random.com", 403},
		{"https://wgexample.io", 403},
		{"https://wgexample.org", 403},
		{"http://subdomain.wgexample.com", 403},
		{"https://api.example.sub.domain.com", 200},
		{"https://service.example.co.uk.com", 200},
		{"https://api.example.domain.com", 403},
		{"https://a.b.c.d.e.com", 200},
	}
	for _, tc := range testCasesList {
		w := performRequest(router, "GET", tc.origin)
		assert.Equalf(t, tc.expectedCode, w.Code, "expected %d for %s, got %d", tc.expectedCode, tc.origin, w.Code)
	}
}

func TestDisabled(t *testing.T) {
	config := Config{
		Enabled:      true,
		AllowOrigins: []string{"https://api.*"},
		AllowMethods: []string{"GET"},
	}

	router := newTestRouter(config)

	w := performRequest(router, "GET", "https://a.test.com")
	assert.Equal(t, 403, w.Code)

	config.Enabled = false
	router = newTestRouter(config)

	w = performRequest(router, "GET", "https://a.test.com")
	assert.Equal(t, 200, w.Code)
}

func BenchmarkCorsWithWildcards(b *testing.B) {
	b.Run("with wildcards", func(b *testing.B) {
		router := newTestRouter(Config{
			Enabled: true,
			AllowOrigins: []string{
				"https://*.example.*.*.com", // multiple sequential wildcards
				"https://*.*.*.*.com",
			},
			AllowMethods: []string{"GET"},
		})

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			w := performRequest(router, "GET", "https://subdomain.test.example.subdomain.example.co.whatgoeshere.woohoo.com")
			assert.Equal(b, 200, w.Code)
		}
	})

	b.Run("with massive wildcards", func(b *testing.B) {
		router := newTestRouter(Config{
			Enabled: true,
			AllowOrigins: []string{
				"https://*.example.*.*.com", // multiple sequential wildcards
				"https://*.*.*.*.com",
			},
			AllowMethods: []string{"GET"},
		})

		longString := strings.Repeat("a", 50000)

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			w := performRequest(router, "GET", fmt.Sprintf("https://%[1]s.%[1]s.%[1]s.%[1]s.com", longString))
			assert.Equal(b, 200, w.Code)
		}
	})

	b.Run("without wildcards", func(b *testing.B) {
		router := newTestRouter(Config{
			Enabled: true,
			AllowOrigins: []string{
				"https://wgexample.com",
			},
			AllowMethods: []string{"GET"},
		})

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			w := performRequest(router, "GET", "https://wgexample.com")
			assert.Equal(b, 200, w.Code)
		}
	})
}
