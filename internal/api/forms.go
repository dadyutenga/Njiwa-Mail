package api

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/njiwa-tz/mail/internal/service"
)

const formBodyMax = 3 << 20

// formCORS answers a browser page on an allowed origin. Others get no CORS
// headers, so their script cannot read the answer (the post itself is also
// refused by SubmitForm).
func (s *Server) formCORS(c fiber.Ctx, formID string) {
	origin := c.Get("Origin")
	if origin == "" {
		return
	}
	origins, err := s.Svc.FormOrigins(reqCtx(c), formID)
	if err != nil {
		return
	}
	n := service.NormalizeOrigin(origin)
	for _, o := range origins {
		if o == n && n != "" {
			c.Set("Access-Control-Allow-Origin", origin)
			c.Set("Vary", "Origin")
			c.Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
			c.Set("Access-Control-Allow-Headers", "Content-Type")
			c.Set("Access-Control-Max-Age", "600")
			return
		}
	}
}

func formPage(title, body string) string {
	return `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>` +
		html.EscapeString(title) + `</title><style>body{font:16px/1.5 system-ui,sans-serif;max-width:32rem;margin:4rem auto;padding:0 1rem;color:#1f2937}</style></head><body><h1>` +
		html.EscapeString(title) + `</h1><p>` + html.EscapeString(body) + `</p><p><a href="javascript:history.back()">Go back</a></p></body></html>`
}

// parseFormBody reads a JSON, urlencoded or multipart body.
func parseFormBody(c fiber.Ctx) (map[string]string, []service.FormFile, bool, error) {
	ct := strings.ToLower(c.Get("Content-Type"))
	bad := func(code, msg string) error { return &service.Error{Status: 422, Code: code, Message: msg} }
	fields := map[string]string{}
	var files []service.FormFile
	switch {
	case strings.HasPrefix(ct, "application/json"):
		var m map[string]any
		if err := json.Unmarshal(c.Body(), &m); err != nil {
			return nil, nil, true, bad("invalid_request", "the body is not a JSON object")
		}
		for k, v := range m {
			switch x := v.(type) {
			case string:
				fields[k] = x
			case float64, bool:
				fields[k] = fmt.Sprint(x)
			case nil:
			default:
				return nil, nil, true, bad("invalid_field", "field values must be text, numbers or true/false")
			}
		}
		return fields, nil, true, nil
	case strings.HasPrefix(ct, "multipart/form-data"):
		form, err := c.MultipartForm()
		if err != nil {
			return nil, nil, false, bad("invalid_request", "the form could not be read")
		}
		for k, v := range form.Value {
			if len(v) > 0 {
				fields[k] = v[0]
			}
		}
		for _, hs := range form.File {
			for _, h := range hs {
				if h.Size > 2<<20 {
					return nil, nil, false, bad("files_too_large", "files may be 2 MiB in all")
				}
				f, err := h.Open()
				if err != nil {
					return nil, nil, false, bad("invalid_request", "a file could not be read")
				}
				data, err := io.ReadAll(io.LimitReader(f, 2<<20+1))
				f.Close()
				if err != nil {
					return nil, nil, false, bad("invalid_request", "a file could not be read")
				}
				files = append(files, service.FormFile{Filename: h.Filename, ContentType: h.Header.Get("Content-Type"), Data: data})
			}
		}
		return fields, files, false, nil
	default: // application/x-www-form-urlencoded
		c.Request().PostArgs().VisitAll(func(k, v []byte) {
			if _, ok := fields[string(k)]; !ok {
				fields[string(k)] = string(v)
			}
		})
		return fields, nil, false, nil
	}
}

func (s *Server) formRoutes(app *fiber.App) {
	svc := s.Svc
	// ---- public: a website posts a form
	app.Options("/v1/forms/:id", func(c fiber.Ctx) error {
		s.formCORS(c, c.Params("id"))
		return c.SendStatus(204)
	})
	app.Get("/v1/forms/:id/nonce", func(c fiber.Ctx) error {
		n, origins, err := svc.IssueFormNonce(reqCtx(c), c.Params("id"))
		if err != nil {
			return writeErr(c, err)
		}
		_ = origins
		s.formCORS(c, c.Params("id"))
		c.Set("Cache-Control", "no-store")
		return writeJSON(c, 200, map[string]any{"nonce": n, "expires_in": 3600})
	})
	app.Post("/v1/forms/:id", func(c fiber.Ctx) error {
		id := c.Params("id")
		s.formCORS(c, id)
		c.Set("Cache-Control", "no-store")
		fail := func(jsonClient bool, err error) error {
			if jsonClient {
				return writeErr(c, err)
			}
			e, ok := service.IsError(err)
			if !ok {
				return writeErr(c, err)
			}
			c.Status(e.Status)
			c.Set(fiber.HeaderContentType, "text/html; charset=utf-8")
			c.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
			msg := e.Message
			if msg == "" {
				msg = "The form could not be sent. Please try again."
			}
			return c.SendString(formPage("Not sent", msg))
		}
		isJSON := strings.HasPrefix(strings.ToLower(c.Get("Content-Type")), "application/json")
		if len(c.Body()) > formBodyMax {
			return fail(isJSON, &service.Error{Status: 413, Code: "body_too_large", Message: "the form is too large"})
		}
		fields, files, jsonClient, err := parseFormBody(c)
		if err != nil {
			return fail(jsonClient || isJSON, err)
		}
		res, err := svc.SubmitForm(reqCtx(c), service.FormPost{FormID: id, Origin: c.Get("Origin"), Referer: c.Get("Referer"),
			IP: s.clientIP(c), Fields: fields, Files: files})
		if err != nil {
			if e, ok := service.IsError(err); ok && e.RetryAfter > 0 {
				c.Set("Retry-After", strconv.Itoa(int(e.RetryAfter/time.Second)+1))
			}
			return fail(jsonClient, err)
		}
		if jsonClient {
			return writeJSON(c, 202, res)
		}
		if res.RedirectURL != "" {
			return c.Redirect().Status(fiber.StatusSeeOther).To(res.RedirectURL)
		}
		c.Set(fiber.HeaderContentType, "text/html; charset=utf-8")
		c.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
		return c.SendString(formPage("Thank you", "Your message was sent."))
	})

	// ---- workspace: forms, filters and the submissions inbox
	base := "/admin/v1/w/:project/:mode"
	h := func(method, path string, fn wsFn) { route(app, method, base+path, s.ws(fn)) }
	ctx := reqCtx
	h("GET", "/forms", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, err := svc.ListForms(ctx(c), sc)
		return map[string]any{"items": items}, err
	})
	h("POST", "/forms", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in service.FormInput
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		id, err := svc.CreateForm(ctx(c), sc, in)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "form.create", "form", id, scoped(sc, map[string]any{"name": in.Name}))
		return map[string]string{"id": id}, nil
	})
	h("GET", "/forms/filters", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, err := svc.ListFormFilters(ctx(c), sc)
		return map[string]any{"items": items}, err
	})
	h("POST", "/forms/filters", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in service.FormFilterInput
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		id, err := svc.CreateFormFilter(ctx(c), sc, in)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "form_filter.create", "form_filter", id, scoped(sc, map[string]any{"kind": in.Kind, "action": in.Action}))
		return map[string]string{"id": id}, nil
	})
	h("POST", "/forms/filters/test", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in service.FormFilterInput
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		return svc.TestFormFilter(ctx(c), sc, in)
	})
	h("PATCH", "/forms/filters/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			Enabled bool `json:"enabled"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		return nil, svc.SetFormFilterEnabled(ctx(c), sc, c.Params("id"), in.Enabled)
	})
	h("DELETE", "/forms/filters/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.DeleteFormFilter(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "form_filter.delete", "form_filter", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("GET", "/forms/submissions", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		items, more, err := svc.ListFormSubmissions(ctx(c), sc, service.FormSubmissionFilter{FormID: c.Query("form"), State: c.Query("state")}, pageOf(c))
		return list(c, items, more, err)
	})
	h("GET", "/forms/submissions/stream", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if !svc.Opts.FormsEnabled {
			return nil, &service.Error{Status: 404, Code: "forms_disabled"}
		}
		after, afterID := time.Now().UTC(), ""
		if last := c.Get("Last-Event-ID"); last != "" {
			if ns, id, ok := strings.Cut(last, ":"); ok {
				if n, err := strconv.ParseInt(ns, 10, 64); err == nil {
					after, afterID = time.Unix(0, n).UTC(), id
				}
			}
		}
		return s.eventStream(c, func(cx context.Context) ([]sseEvent, error) {
			items, err := svc.FormSubmissionsSince(cx, sc.EnvID, after, afterID, 50)
			out := make([]sseEvent, 0, len(items))
			for _, it := range items {
				after, afterID = it.ReceivedAt, it.ID
				out = append(out, sseEvent{Name: "submission", ID: strconv.FormatInt(it.ReceivedAt.UnixNano(), 10) + ":" + it.ID, Data: it})
			}
			return out, err
		}), nil
	})
	h("GET", "/forms/submissions/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		return svc.GetFormSubmission(ctx(c), sc, c.Params("id"))
	})
	h("PATCH", "/forms/submissions/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			State      *string `json:"state"`
			AssignedTo *string `json:"assigned_to"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.UpdateFormSubmission(ctx(c), sc, c.Params("id"), in.State, in.AssignedTo); err != nil {
			return nil, err
		}
		s.audit(c, u, "form_submission.update", "form_submission", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("DELETE", "/forms/submissions/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.DeleteFormSubmission(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "form_submission.delete", "form_submission", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("POST", "/forms/submissions/:id/reply", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in struct {
			Subject string `json:"subject"`
			Text    string `json:"text"`
		}
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		acc, err := svc.ReplyToFormSubmission(ctx(c), sc, u.Email, c.Params("id"), in.Subject, in.Text)
		if err != nil {
			return nil, err
		}
		s.audit(c, u, "form_submission.reply", "form_submission", c.Params("id"), scoped(sc, map[string]any{"message_id": acc.ID}))
		return acc, nil
	})
	h("GET", "/forms/submissions/:id/files/:fid", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		f, err := svc.OpenFormFile(ctx(c), sc, c.Params("id"), c.Params("fid"))
		if err != nil {
			return nil, err
		}
		f.Size = len(f.Data)
		return download(f), nil
	})
	h("PUT", "/forms/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		var in service.FormInput
		if err := decode(c, &in); err != nil {
			return nil, err
		}
		if err := svc.UpdateForm(ctx(c), sc, c.Params("id"), in); err != nil {
			return nil, err
		}
		s.audit(c, u, "form.update", "form", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
	h("DELETE", "/forms/:id", func(c fiber.Ctx, u *service.AdminUser, sc *service.Scope) (any, error) {
		if err := svc.DeleteForm(ctx(c), sc, c.Params("id")); err != nil {
			return nil, err
		}
		s.audit(c, u, "form.delete", "form", c.Params("id"), scoped(sc, nil))
		return nil, nil
	})
}
