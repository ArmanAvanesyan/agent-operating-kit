package sdk

import (
	"bytes"
	"context"
	"errors"
	i "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/integrationv1"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
	"sync/atomic"
	"time"
)

type OperationalReadBackend interface {
	Current(context.Context, i.OperationalReadRequest, i.Policy, i.Enrollment) error
	Transport(context.Context, i.OperationalReadRequest, []byte, func(context.Context) error) (readsource.Transport, error)
}

func (s *Service) WithOperationalReadBackend(b OperationalReadBackend) *Service {
	v := *s
	v.operationalReads = b
	return &v
}

type integrationBudgetKey struct{}
type integrationBudget struct {
	requests, bytes, items atomic.Int64
	limits                 i.Limits
}

func OperationalEndpointSHA(source readsource.Source, request readsource.Request) string {
	return SHA256(engine.Encode(struct{ Provider, Origin, API, Kind, Method string }{source.Provider, source.Origin, source.APIVersion, request.Kind, request.Method}))
}

type guardedOperationalRead struct {
	inner   readsource.Transport
	request i.OperationalReadRequest
	guard   func(context.Context) error
}

func (t guardedOperationalRead) Do(ctx context.Context, source readsource.Source, request readsource.Request) (readsource.Response, error) {
	if e := t.guard(ctx); e != nil {
		return readsource.Response{}, e
	}
	allowed := false
	for _, sha := range t.request.AllowedEndpointSHA256 {
		allowed = allowed || sha == OperationalEndpointSHA(source, request)
	}
	if !allowed {
		return readsource.Response{}, errors.New("operational read endpoint not enrolled")
	}
	budget, _ := ctx.Value(integrationBudgetKey{}).(*integrationBudget)
	if budget != nil && budget.requests.Add(1) > int64(budget.limits.Requests) {
		return readsource.Response{}, errors.New("shared cycle request budget exceeded")
	}
	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(t.request.Limits.RequestSeconds)*time.Second)
	defer cancel()
	if e := t.guard(reqCtx); e != nil {
		return readsource.Response{}, e
	}
	res, e := t.inner.Do(reqCtx, source, request)
	if e != nil {
		return res, e
	}
	if len(res.Body) > t.request.Limits.ResponseBytes || budget != nil && len(res.Body) > budget.limits.ResponseBytes {
		return readsource.Response{}, errors.New("operational response bound")
	}
	if budget != nil && budget.bytes.Add(int64(len(res.Body))) > int64(budget.limits.CaptureBytes) {
		return readsource.Response{}, errors.New("shared cycle byte budget exceeded")
	}
	return res, nil
}

// CaptureOperational keeps the P2 imported-parser bundle unqualified. Its new
// operational receipt is separate; it never changes QualificationOnly semantics.
// The transport factory must install guard after credential resolution as well.
func (s *Service) CaptureOperational(ctx context.Context, requestPath, configuration, policy, enrollment, previous, out string) (r.Manifest, error) {
	var result r.Manifest
	if s.operationalReads == nil {
		return result, operationFailure("integration.read-unavailable", "operational read bootstrap missing", 3, nil)
	}
	raw, e := ReadLocal(ctx, requestPath)
	if e != nil {
		return result, e
	}
	var request i.OperationalReadRequest
	if DecodeRead(raw, &request) != nil || request.Version != i.Version || request.Kind != "OperationalReadRequest" || ValidateIntegrationLimits(request.Limits) != nil || request.RouteID == "" || len(request.AllowedEndpointSHA256) == 0 || len(request.AllowedEndpointSHA256) > 100 {
		return result, errors.New("invalid operational read scope")
	}
	cfg, configRaw, e := LoadReadConfiguration(ctx, configuration)
	if e != nil {
		return result, e
	}
	if request.ConnectionSHA256 != SHA256(configRaw) || request.Principal != cfg.Source.PrincipalPseudonym || request.AuthenticationMode != cfg.Source.Auth.Mode || request.Origin != cfg.Source.Origin || request.QuerySHA256 != SHA256(engine.Encode(cfg.Source.Scope)) {
		return result, errors.New("operational read principal/configuration/query mismatch")
	}
	praw, e := ReadLocal(ctx, policy)
	if e != nil {
		return result, e
	}
	eraw, e := ReadLocal(ctx, enrollment)
	if e != nil {
		return result, e
	}
	p, en, e := ValidateIntegrationAuthority(praw, eraw, SHA256(raw), "operational-read", time.Now().UTC())
	if e != nil || en.OutputPath != out || en.RouteID != request.RouteID {
		return result, errors.New("operational read authority mismatch")
	}
	if request.ConfigurationSHA256 != p.Routes[request.RouteID] {
		return result, errors.New("operational read route revision mismatch")
	}
	if !integrationLimitsFit(request.Limits, p.Limits) {
		return result, errors.New("operational request exceeds policy limits")
	}
	guard := func(c context.Context) error {
		if err := c.Err(); err != nil {
			return err
		}
		if _, _, err := ValidateIntegrationAuthority(praw, eraw, SHA256(raw), "operational-read", time.Now().UTC()); err != nil {
			return err
		}
		return s.operationalReads.Current(c, request, p, en)
	}
	if e = guard(ctx); e != nil {
		return result, e
	}
	var prior *ReadBundle
	if previous != "" {
		prior, e = LoadReadBundle(ctx, previous)
		if e != nil || prior.digest != request.CheckpointSHA256 || !bytes.Equal(prior.configBytes, configRaw) {
			return result, errors.New("operational checkpoint scope/account/configuration changed")
		}
	} else if request.CheckpointSHA256 != "" {
		return result, errors.New("missing pinned checkpoint")
	}
	factory, e := s.operationalReads.Transport(ctx, request, append([]byte(nil), configRaw...), guard)
	if e != nil || factory == nil {
		return result, errors.New("approved operational read transport unavailable")
	}
	if e = guard(ctx); e != nil {
		return result, e
	}
	w, e := engine.ReserveBundle(ctx, out)
	if e != nil {
		return result, e
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(request.Limits.CycleSeconds)*time.Second)
	defer cancel()
	if _, ok := ctx.Value(integrationBudgetKey{}).(*integrationBudget); !ok {
		ctx = context.WithValue(ctx, integrationBudgetKey{}, &integrationBudget{limits: request.Limits})
	}
	var priorSource *readsource.Capture
	if prior != nil {
		priorSource = prior.capture
	}
	captured, e := readsource.CaptureRead(ctx, cfg.Source, "capture", priorSource, guardedOperationalRead{factory, request, guard})
	if e != nil {
		return result, e
	}
	if budget, _ := ctx.Value(integrationBudgetKey{}).(*integrationBudget); budget != nil && budget.items.Add(int64(len(captured.Snapshot.Items))) > int64(budget.limits.Items) {
		return result, errors.New("shared cycle item budget exceeded")
	}
	result, files, e := buildReadBundle(configRaw, captured, r.TransportIdentity{Kind: "captured-source", Version: "unverified", Principal: cfg.Source.PrincipalPseudonym, AuthMode: cfg.Source.Auth.Mode}, cfg.Source.ReadPolicySHA256, "")
	if e != nil {
		return result, e
	}
	receipt := i.OperationalReadObservation{Version: i.Version, Kind: "OperationalReadObservation", RequestSHA256: SHA256(raw), ParserBundleSHA256: SHA256(files["bundle.json"]), ObservedAt: captured.Snapshot.ObservedAt, Request: request, Policy: p, Enrollment: en}
	outer := map[string][]byte{"operational-read.json": engine.Encode(receipt), "assets/request.json": raw, "assets/policy.json": praw, "assets/enrollment.json": eraw}
	for name, data := range files {
		outer["capture/"+name] = data
	}
	files = operationalReportFiles(i.Version, receipt, outer)
	preserve, stop := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer stop()
	if e = w.Write(preserve, files); e != nil {
		return result, e
	}
	return result, w.Sync(preserve)
}
