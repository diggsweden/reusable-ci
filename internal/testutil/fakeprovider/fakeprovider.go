// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package fakeprovider is an in-memory implementation of provider.Provider
// for app-layer tests. Tests construct it with the responses they want and
// inspect Calls afterwards to assert how the use case interacted with it.
// New Provider methods get matching fields here.
package fakeprovider

import (
	"context"
	"sync"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

// Calls records every method invocation count for assertions.
type Calls struct {
	Name                   int
	ResolveContext         int
	FetchRepoMetadata      int
	ValidateToken          int
	ValidateBotPermissions int
	CreateRelease          int
}

// TokenCall records arguments passed to ValidateToken.
type TokenCall struct{ Token, Repo string }

// Fake implements provider.Provider with configurable returns.
type Fake struct {
	t             *testing.T
	platform      provider.Platform
	eventCtx      *provider.EventContext
	resolveErr    error
	repoMeta      *provider.RepoMetadata
	repoErr       error
	repoArgs      []string
	tokenErr      error
	tokenCalls    []TokenCall
	botPerms      *provider.BotPermissions
	botErr        error
	botArgs       []string
	createRelErr  error
	createRelArgs []ReleaseCall
	uploadErr     error
	uploadCalls   []provider.SARIFUpload
	mu            sync.Mutex
	calls         Calls
}

// ReleaseCall captures a single CreateRelease invocation.
type ReleaseCall struct {
	Repo string
	Spec provider.ReleaseSpec
}

// New returns a Fake defaulting to PlatformLocal with no event context.
// Chainable setters configure the responses.
func New(t *testing.T) *Fake {
	t.Helper()
	return &Fake{
		t:        t,
		platform: provider.PlatformLocal,
	}
}

// WithPlatform sets the platform Name() returns.
func (f *Fake) WithPlatform(p provider.Platform) *Fake {
	f.platform = p
	return f
}

// WithEventContext configures the EventContext that ResolveContext returns.
func (f *Fake) WithEventContext(evt provider.EventContext) *Fake {
	f.eventCtx = &evt
	return f
}

// WithResolveContextError makes ResolveContext return the given error.
func (f *Fake) WithResolveContextError(err error) *Fake {
	f.resolveErr = err
	return f
}

// WithRepoMetadata configures the RepoMetadata that FetchRepoMetadata returns.
func (f *Fake) WithRepoMetadata(m provider.RepoMetadata) *Fake {
	f.repoMeta = &m
	return f
}

// WithFetchRepoMetadataError makes FetchRepoMetadata return the given error.
func (f *Fake) WithFetchRepoMetadataError(err error) *Fake {
	f.repoErr = err
	return f
}

// FetchRepoArgs returns the repo argument passed to each FetchRepoMetadata
// call, in order. Empty when never called.
func (f *Fake) FetchRepoArgs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.repoArgs))
	copy(out, f.repoArgs)
	return out
}

// WithValidateTokenError makes ValidateToken return the given error.
func (f *Fake) WithValidateTokenError(err error) *Fake {
	f.tokenErr = err
	return f
}

// ValidateTokenCalls returns the (token, repo) tuples passed to each
// ValidateToken call, in order.
func (f *Fake) ValidateTokenCalls() []TokenCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]TokenCall, len(f.tokenCalls))
	copy(out, f.tokenCalls)
	return out
}

// WithBotPermissions configures the BotPermissions ValidateBotPermissions returns.
func (f *Fake) WithBotPermissions(p provider.BotPermissions) *Fake {
	f.botPerms = &p
	return f
}

// WithValidateBotPermissionsError makes ValidateBotPermissions return the given error.
func (f *Fake) WithValidateBotPermissionsError(err error) *Fake {
	f.botErr = err
	return f
}

// ValidateBotPermissionsArgs returns the repo arguments passed to each
// ValidateBotPermissions call.
func (f *Fake) ValidateBotPermissionsArgs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.botArgs))
	copy(out, f.botArgs)
	return out
}

// Calls returns a snapshot of the per-method invocation counters.
func (f *Fake) Calls() Calls {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// Name implements provider.Provider.
func (f *Fake) Name() provider.Platform {
	f.mu.Lock()
	f.calls.Name++
	f.mu.Unlock()
	return f.platform
}

// ResolveContext implements provider.Provider.
func (f *Fake) ResolveContext(_ context.Context) (*provider.EventContext, error) {
	f.mu.Lock()
	f.calls.ResolveContext++
	f.mu.Unlock()
	if f.resolveErr != nil {
		return nil, f.resolveErr
	}
	if f.eventCtx == nil {
		return &provider.EventContext{Platform: f.platform}, nil
	}
	cp := *f.eventCtx
	if cp.Platform == "" {
		cp.Platform = f.platform
	}
	return &cp, nil
}

// FetchRepoMetadata implements provider.Provider.
func (f *Fake) FetchRepoMetadata(_ context.Context, repo string) (*provider.RepoMetadata, error) {
	f.mu.Lock()
	f.calls.FetchRepoMetadata++
	f.repoArgs = append(f.repoArgs, repo)
	f.mu.Unlock()
	if f.repoErr != nil {
		return nil, f.repoErr
	}
	if f.repoMeta == nil {
		return &provider.RepoMetadata{}, nil
	}
	cp := *f.repoMeta
	return &cp, nil
}

// ValidateToken implements provider.Provider.
func (f *Fake) ValidateToken(_ context.Context, token, repo string) error {
	f.mu.Lock()
	f.calls.ValidateToken++
	f.tokenCalls = append(f.tokenCalls, TokenCall{Token: token, Repo: repo})
	err := f.tokenErr
	f.mu.Unlock()
	return err
}

// ValidateBotPermissions implements provider.Provider.
func (f *Fake) ValidateBotPermissions(_ context.Context, repo string) (*provider.BotPermissions, error) {
	f.mu.Lock()
	f.calls.ValidateBotPermissions++
	f.botArgs = append(f.botArgs, repo)
	f.mu.Unlock()
	if f.botErr != nil {
		return nil, f.botErr
	}
	if f.botPerms == nil {
		return &provider.BotPermissions{}, nil
	}
	cp := *f.botPerms
	return &cp, nil
}

// WithCreateReleaseError makes CreateRelease return the given error.
func (f *Fake) WithCreateReleaseError(err error) *Fake {
	f.createRelErr = err
	return f
}

// CreateReleaseCalls returns each (repo, spec) tuple passed to CreateRelease.
func (f *Fake) CreateReleaseCalls() []ReleaseCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]ReleaseCall, len(f.createRelArgs))
	copy(out, f.createRelArgs)
	return out
}

// CreateRelease implements provider.Provider.
func (f *Fake) CreateRelease(_ context.Context, repo string, spec provider.ReleaseSpec) error {
	f.mu.Lock()
	f.calls.CreateRelease++
	f.createRelArgs = append(f.createRelArgs, ReleaseCall{Repo: repo, Spec: spec})
	err := f.createRelErr
	f.mu.Unlock()
	return err
}

// WithUploadSARIFError makes UploadSARIF return the given error.
func (f *Fake) WithUploadSARIFError(err error) *Fake {
	f.uploadErr = err
	return f
}

// UploadSARIFCalls returns each SARIFUpload value passed to UploadSARIF.
func (f *Fake) UploadSARIFCalls() []provider.SARIFUpload {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]provider.SARIFUpload, len(f.uploadCalls))
	copy(out, f.uploadCalls)
	return out
}

// UploadSARIF implements provider.Provider.
func (f *Fake) UploadSARIF(_ context.Context, up provider.SARIFUpload) error {
	f.mu.Lock()
	f.uploadCalls = append(f.uploadCalls, up)
	err := f.uploadErr
	f.mu.Unlock()
	return err
}

// Compile-time conformance check.
var _ provider.Provider = (*Fake)(nil)
