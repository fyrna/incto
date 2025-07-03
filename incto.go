// ultra stupid web framework
package incto

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type HandlerFunc func(Ctx) error
type MiddlewareFunc func(HandlerFunc) HandlerFunc

type Magic struct {
	routes      []Route
	middlewares []MiddlewareFunc
	server      *http.Server
}

type Route struct {
	Method      string
	Path        string
	Handler     HandlerFunc
	Middlewares []MiddlewareFunc
	pathRegex   *regexp.Regexp
	paramNames  []string
}

type Spell struct {
	magic       *Magic
	method      string
	path        string
	middlewares []MiddlewareFunc
}

type Experiment struct {
	name        string
	magic       *Magic
	conditions  []ExperimentCondition
	middlewares []MiddlewareFunc
}

type ExperimentCondition interface {
	Apply(*Experiment) *Experiment
}

type PathPrefixCondition struct {
	prefix string
}

func (p PathPrefixCondition) Apply(exp *Experiment) *Experiment {
	return exp
}

type Ctx interface {
	// request data
	Param(key string) string
	Query(key string) string
	Form(key string) string
	Header(key string) string

	// request body
	Bind(v any) error

	// response
	String(code int, text string) error
	JSON(code int, obj any) error
	HTML(code int, html string) error

	// context
	Set(key string, value any)
	Get(key string) any
	Context() context.Context

	// internal
	Request() *http.Request
	ResponseWriter() http.ResponseWriter
}

// context implementation
type Context struct {
	req    *http.Request
	writer http.ResponseWriter
	params map[string]string
	store  map[string]any
	ctx    context.Context
}

func newContext(w http.ResponseWriter, r *http.Request) *Context {
	return &Context{
		req:    r,
		writer: w,
		params: make(map[string]string),
		store:  make(map[string]any),
		ctx:    r.Context(),
	}
}

func (c *Context) Param(key string) string {
	return c.params[key]
}

func (c *Context) Query(key string) string {
	return c.req.URL.Query().Get(key)
}

func (c *Context) Form(key string) string {
	return c.req.FormValue(key)
}

func (c *Context) Header(key string) string {
	return c.req.Header.Get(key)
}

func (c *Context) Bind(v any) error {
	contentType := c.Header("Content-Type")

	if strings.Contains(contentType, "application/json") {
		return json.NewDecoder(c.req.Body).Decode(v)
	}

	if strings.Contains(contentType, "application/x-www-form-urlencoded") {
		if err := c.req.ParseForm(); err != nil {
			return err
		}

		// simple form binding
		// TODO: improve this
		if m, ok := v.(*map[string]any); ok {
			*m = make(map[string]any)
			for k, v := range c.req.Form {
				if len(v) > 0 {
					(*m)[k] = v[0]
				}
			}
		}
	}

	return fmt.Errorf("unsupported content type: %s", contentType)
}

func (c *Context) String(code int, text string) error {
	c.writer.Header().Set("Content-Type", "text/plain")
	c.writer.WriteHeader(code)
	_, err := c.writer.Write([]byte(text))
	return err
}

func (c *Context) JSON(code int, obj any) error {
	c.writer.Header().Set("Content-Type", "application/json")
	c.writer.WriteHeader(code)
	return json.NewEncoder(c.writer).Encode(obj)
}

func (c *Context) HTML(code int, html string) error {
	c.writer.Header().Set("Content-Type", "text/html")
	c.writer.WriteHeader(code)
	_, err := c.writer.Write([]byte(html))
	return err
}

func (c *Context) Set(key string, value any) {
	c.store[key] = value
}

func (c *Context) Get(key string) any {
	return c.store[key]
}

func (c *Context) Context() context.Context {
	return c.ctx
}

func (c *Context) Request() *http.Request {
	return c.req
}

func (c *Context) ResponseWriter() http.ResponseWriter {
	return c.writer
}

func SpellMagic() *Magic {
	return &Magic{
		routes: make([]Route, 0),
	}
}

func (m *Magic) Spell(methodPath string) *Spell {
	parts := strings.SplitN(methodPath, " ", 2)

	// what the hell are u doing?
	if len(parts) != 2 {
		panic("Spell format should be 'METHOD /path', got: " + methodPath)
	}

	method := strings.ToUpper(parts[0])
	path := parts[1]

	return &Spell{
		magic:  m,
		method: method,
		path:   path,
	}
}

func (s *Spell) With(handler HandlerFunc) *Spell {
	// compile path regex for parameters
	pathRegex, paramNames := compilePath(s.path)

	route := Route{
		Method:      s.method,
		Path:        s.path,
		Handler:     handler,
		Middlewares: s.middlewares,
		pathRegex:   pathRegex,
		paramNames:  paramNames,
	}

	s.magic.routes = append(s.magic.routes, route)
	return s
}

func (s *Spell) Require(middleware MiddlewareFunc) *Spell {
	s.middlewares = append(s.middlewares, middleware)
	return s
}

// cast function to start multiple spells
func Cast(spells ...*Spell) *Magic {
	if len(spells) == 0 {
		panic("Cast requires at least one spell")
	}

	return spells[0].magic
}

func (m *Magic) Start(addr string) error {
	mux := http.NewServeMux()

	// register all routes
	mux.HandleFunc("/", m.handleRequest)

	m.server = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	fmt.Printf("🪄 Magic is brewing at %s\n", addr)
	return m.server.ListenAndServe()
}

func (m *Magic) handleRequest(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(w, r)

	// find matching route
	for _, route := range m.routes {
		if route.Method != r.Method {
			continue
		}

		if matches := route.pathRegex.FindStringSubmatch(r.URL.Path); matches != nil {
			// extract parameters
			for i, name := range route.paramNames {
				ctx.params[name] = matches[i+1]
			}

			// build handler chain
			handler := route.Handler

			// apply route-specific middlewares (reverse order)
			for i := len(route.Middlewares) - 1; i >= 0; i-- {
				handler = route.Middlewares[i](handler)
			}

			// apply global middlewares (reverse order)
			for i := len(m.middlewares) - 1; i >= 0; i-- {
				handler = m.middlewares[i](handler)
			}

			// execute handler
			if err := handler(ctx); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
	}

	// No route found
	// TODO: add cute custom 404
	http.NotFound(w, r)
}

// experiment : test your divine power
func Experiments(name string) *Experiment {
	return &Experiment{
		name:  name,
		magic: SpellMagic(),
	}
}

// what you would like to sacrifice ?
func (e *Experiment) Given(conditions ...ExperimentCondition) *Experiment {
	e.conditions = append(e.conditions, conditions...)
	return e
}

func (e *Experiment) Subject(spells ...*Spell) *Experiment {
	// apply conditions to all spells
	for _, condition := range e.conditions {
		condition.Apply(e)
	}

	// give your power (add spells to magic)
	for _, spell := range spells {
		spell.magic = e.magic
	}

	return e
}

func PathPrefix(prefix string) ExperimentCondition {
	return PathPrefixCondition{prefix: prefix}
}

// helper function to compile path with parameters
func compilePath(path string) (*regexp.Regexp, []string) {
	var paramNames []string

	// convert /users/:id/posts/:postId
	// to      /users/([^/]+)/posts/([^/]+)
	regexPath := regexp.MustCompile(`:([a-zA-Z_][a-zA-Z0-9_]*)`).ReplaceAllStringFunc(path, func(match string) string {
		paramName := strings.TrimPrefix(match, ":")
		paramNames = append(paramNames, paramName)
		return `([^/]+)`
	})

	// escape special regex characters except our parameter patterns
	regexPath = "^" + regexPath + "$"

	compiled, err := regexp.Compile(regexPath)
	if err != nil {
		panic(fmt.Sprintf("Invalid path pattern '%s': %v", path, err))
	}

	return compiled, paramNames
}
