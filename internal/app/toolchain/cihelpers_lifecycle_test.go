// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

const changelogMiseBytes = "inert pinned mise fixture; never execute these bytes\n"

type changelogTransport func(*http.Request) (*http.Response, error)

func (f changelogTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// Every case, including refusal controls, blocks the real transport. No parallel tests.
func blockChangelogHTTP(t *testing.T, reply func(*http.Request) (*http.Response, error)) {
	t.Helper()

	previous := http.DefaultTransport
	http.DefaultTransport = changelogTransport(reply)

	t.Cleanup(func() { http.DefaultTransport = previous })
}

func changelogTempDir(t *testing.T) string {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	return root
}

func changelogFixtureRoot(t *testing.T) string {
	t.Helper()

	root := changelogTempDir(t)
	for _, pair := range [][2]string{{"HOME", "home"}, {runnerPathName, "empty-path"}, {"TMPDIR", "os-temp"}, {"TEMP", "os-temp"}, {"TMP", "os-temp"}} {
		dir := filepath.Join(root, pair[1])
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}

		t.Setenv(pair[0], dir)
	}

	blockChangelogHTTP(t, func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusForbidden, Body: http.NoBody, Header: make(http.Header)}, nil
	})

	return root
}

func changelogArchive(t *testing.T) ([]byte, string) {
	t.Helper()

	var body bytes.Buffer

	gz := gzip.NewWriter(&body)

	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "mise/bin/mise", Typeflag: tar.TypeReg, Mode: 0o600, Size: int64(len(changelogMiseBytes))}); err != nil {
		t.Fatal(err)
	}

	if _, err := io.WriteString(tw, changelogMiseBytes); err != nil {
		t.Fatal(err)
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	return body.Bytes(), fmt.Sprintf("%x", sha256.Sum256(body.Bytes()))
}

type changelogLifecycleRunner struct {
	setBin func(string)
	run    func(context.Context, []string, ...string) (string, error)
}

func (r changelogLifecycleRunner) SetBin(bin string) { r.setBin(bin) }
func (r changelogLifecycleRunner) Run(ctx context.Context, env []string, args ...string) (string, error) {
	return r.run(ctx, env, args...)
}
func (r changelogLifecycleRunner) RunInherit(ctx context.Context, env []string, args ...string) error {
	_, err := r.run(ctx, env, append([]string{"UNEXPECTED-RunInherit"}, args...)...)

	return err
}

func TestInstallChangelogRenderer_LocalRefusalBeforeEffects(t *testing.T) { //nolint:gocognit,gocyclo // public entry and all owned state are checked for each local obstacle.
	for _, scenario := range []string{"nil runner", "empty PATH", "backend", "renderer pin", "mise pin", "invalid run ID", "PATH in reset", "bin in reset", "mise in reset", "PATH is mise", "PATH hardlinks mise", "PATH selected leaf", "PATH directory", "PATH missing parent", "linked bin", "linked reset", "mise leaf directory", "bin separator", "reset obstacle", "reset inside selected leaf", "mise inside selected leaf", "scratch separator", "scratch raw separator", "explicit scratch link", "malformed URL", "URL fragment", "URL empty fragment", "URL query", "URL empty query", "selected directory", "missing OS temp fallback"} {
		t.Run(scenario, func(t *testing.T) {
			root := changelogFixtureRoot(t)
			archive, pin := changelogArchive(t)
			in := InstallChangelogRendererInput{Backend: gitCliffBin, GitCliffVersion: "2.6.1", RunID: "42",
				BinHome: filepath.Join(root, "new", "bin"), PathFile: filepath.Join(root, runnerPathName), RunnerTemp: filepath.Join(root, "temp"),
				Mise: InstallMiseInput{Version: "2026.1.2", LinuxX64SHA256: pin, LinuxARM64SHA256: pin, DestDir: filepath.Join(root, "mise-bin"), BaseURL: "https://fixture.invalid/mise"}}
			prepare := filepath.Join(in.RunnerTemp, "reusable-ci-prepare-mise-42")
			writeChangelogCanary(t, filepath.Join(prepare, "data", "stale"), "owned stale tree\n", 0o600)
			writeChangelogCanary(t, in.PathFile, "caller PATH bytes\n", 0o640)

			wantErr, reason := errs.ErrValidation, "inspect planned mise directory"

			switch scenario {
			case "nil runner":
				wantErr, reason = errs.ErrUsage, "runner is required"
			case "empty PATH":
				in.PathFile, wantErr, reason = "", errs.ErrUsage, "path-file is required"
			case "backend":
				in.Backend, reason = "aqua:unselected/tool", "backend must be"
			case "renderer pin":
				in.GitCliffVersion, wantErr, reason = "latest", errs.ErrUsage, "git-cliff version must be exact"
			case "mise pin":
				in.Mise.LinuxARM64SHA256, wantErr, reason = "", errs.ErrUsage, "SHA-256 pins are required"
			case "invalid run ID":
				in.RunID, in.Mise.BaseURL, wantErr = "../../..", "://parsing-tripwire%", errs.ErrUsage
				reason = "run-id must contain"
			case "PATH in reset":
				reason = "prepare reset overlaps caller path"
				in.PathFile = filepath.Join(prepare, runnerPathName)
				writeChangelogCanary(t, in.PathFile, "caller PATH inside scratch\n", 0o640)
			case "bin in reset":
				reason = "prepare reset overlaps caller path"
				in.BinHome = filepath.Join(prepare, "caller-bin")
				writeChangelogCanary(t, filepath.Join(in.BinHome, "keep"), "caller bin\n", 0o600)
			case "mise in reset":
				reason = "prepare reset overlaps caller path"
				in.Mise.DestDir = filepath.Join(prepare, "caller-mise")
				writeChangelogCanary(t, filepath.Join(in.Mise.DestDir, "mise"), "caller mise\n", 0o700)
			case "PATH is mise":
				reason = "files alias"
				in.PathFile = filepath.Join(in.Mise.DestDir, "mise")
				writeChangelogCanary(t, in.PathFile, "caller PATH\n", 0o640)
			case "PATH hardlinks mise":
				reason = "files alias"
				misePath := filepath.Join(in.Mise.DestDir, "mise")
				writeChangelogCanary(t, misePath, "caller mise\n", 0o700)

				in.PathFile = filepath.Join(root, "hardlink")
				if err := os.Link(misePath, in.PathFile); err != nil {
					t.Fatal(err)
				}
			case "PATH selected leaf":
				in.PathFile = filepath.Join(in.BinHome, gitCliffBin)
				reason = "overlap an executable destination"
			case "PATH directory":
				in.PathFile = filepath.Join(root, "path-directory")
				if err := os.Mkdir(in.PathFile, 0o700); err != nil {
					t.Fatal(err)
				}

				reason = "nonlinked regular file"
			case "PATH missing parent":
				in.PathFile, wantErr = filepath.Join(root, "unplanned", runnerPathName), os.ErrNotExist
				reason = "open runner/install parent"
			case "linked bin":
				in.BinHome = filepath.Join(root, "linked-bin")
				changelogSymlink(t, root, in.BinHome)
			case "linked reset":
				in.RunID = "linked"
				changelogSymlink(t, prepare, filepath.Join(in.RunnerTemp, "reusable-ci-prepare-mise-linked"))
			case "mise leaf directory":
				reason = "nonlinked regular file"

				writeChangelogCanary(t, filepath.Join(in.Mise.DestDir, "mise", "keep"), "caller child\n", 0o600)
			case "bin separator":
				in.BinHome += ":other"
				reason = "invalid mise local path"
			case "reset obstacle":
				in.RunnerTemp = filepath.Join(root, "obstacle")
				writeChangelogCanary(t, in.RunnerTemp, "not a directory\n", 0o600)
			case "reset inside selected leaf":
				in.RunnerTemp = filepath.Join(in.BinHome, gitCliffBin)
				reason = "overlap an executable destination"
			case "mise inside selected leaf":
				in.Mise.DestDir = filepath.Join(in.BinHome, gitCliffBin, "mise-bin")
				reason = "overlap an executable destination"
			case "scratch separator":
				in.RunnerTemp += ":other"
				reason = "invalid mise local path"
			case "scratch raw separator":
				in.RunnerTemp += "/unsafe:component/../scratch"
				reason = "invalid mise local path"
			case "explicit scratch link":
				alias := filepath.Join(root, "scratch-alias")
				changelogSymlink(t, in.RunnerTemp, alias)
				in.RunnerTemp = alias
			case "malformed URL":
				in.Mise.BaseURL, wantErr, reason = "://malformed%", errs.ErrUsage, "invalid mise download URL"
			case "URL fragment", "URL empty fragment", "URL query", "URL empty query":
				in.Mise.BaseURL += map[string]string{"URL fragment": "#ignored", "URL empty fragment": "#", "URL query": "?mirror=1", "URL empty query": "?"}[scenario]
				wantErr, reason = errs.ErrUsage, "directory prefix"
			case "selected directory":
				writeChangelogCanary(t, filepath.Join(in.BinHome, gitCliffBin, "keep"), "selected directory child\n", 0o600)

				reason = "regular file or symlink"
			case "missing OS temp fallback":
				in.RunnerTemp, wantErr, reason = "", os.ErrNotExist, "resolve OS temp directory"

				for _, key := range []string{"TMPDIR", "TEMP", "TMP"} {
					t.Setenv(key, filepath.Join(root, "missing-os-temp"))
				}
			}

			var events []string

			blockChangelogHTTP(t, func(req *http.Request) (*http.Response, error) {
				events = append(events, fmt.Sprintf("HTTP %s path=%q uri=%q", req.URL.String(), req.URL.Path, req.URL.RequestURI()))
				// Even a regression that reaches download receives only inert bytes.
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(archive)), Header: make(http.Header)}, nil
			})

			var runner MiseRunner = changelogLifecycleRunner{setBin: func(bin string) { events = append(events, "SetBin "+bin) },
				run: func(context.Context, []string, ...string) (string, error) {
					events = append(events, "Run")

					return "", errs.ErrUnsupported
				}}
			if scenario == "nil runner" {
				runner = nil
			}

			unchanged := changelogUnchanged(t, root)

			var out bytes.Buffer

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			err := InstallChangelogRenderer(ctx, runner, &out, in)
			if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), reason) {
				t.Errorf("refusal = %v, want %v containing %q", err, wantErr, reason)
			}

			if len(events) != 0 || out.Len() != 0 {
				t.Errorf("effects before refusal: events=%v output=%q", events, out.String())
			}

			unchanged()
		})
	}
}

// Keep assertions on exact event slices readable in the lifecycle matrix below.
func assertChangelogEvents(t *testing.T, got, want []string) {
	t.Helper()

	if !reflect.DeepEqual(got, want) {
		t.Errorf("events = %q, want %q", got, want)
	}
}

func TestInstallChangelogRenderer_HermeticLifecycle(t *testing.T) { //nolint:gocognit,gocyclo,maintidx // one public-flow fixture binds each phase and its documented partial state.
	// These cases replace DefaultTransport and process env; none may run in parallel.
	for _, scenario := range []string{"git-cliff success", "git-chglog defaults", "OS temp fallback", "linked OS temp fallback", "existing selected link", "HTTP denied", "checksum mismatch", "malformed archive", "late install obstacle", "install failure", "where failure", "empty where", "where outside root", "missing binary", "occupied publication", "late PATH obstacle", "version failure", "version canceled"} {
		t.Run(scenario, func(t *testing.T) {
			root := changelogFixtureRoot(t)
			t.Chdir(root)
			home := filepath.Join(root, "home")
			emptyPath := filepath.Join(root, "empty-path")
			t.Setenv(changelogRecorderRoot, root)
			t.Setenv("REUSABLE_CI_OWNED_CHANGELOG_FAIL", "no")
			t.Setenv("GROUP28_RAW", "  raw:value=\u00e5\t\nend  ")

			for _, name := range []string{miseCacheKey, miseConfigKey, miseDataKey, miseStateKey} {
				t.Setenv(name, "ambient:"+name+"=raw value")
			}

			if scenario == "version failure" {
				t.Setenv("REUSABLE_CI_OWNED_CHANGELOG_FAIL", "yes")
			}

			archive, pin := changelogArchive(t)
			if scenario == "malformed archive" {
				archive = []byte("pinned but not gzip")
				pin = fmt.Sprintf("%x", sha256.Sum256(archive))
			}

			binHome, pathFile := filepath.Join(root, "renderer bin"), filepath.Join(root, runnerPathName)
			in := InstallChangelogRendererInput{Backend: gitCliffBin, GitCliffVersion: "2.6.1", GitChglogVersion: "0.15.4", RunID: "42",
				BinHome: binHome, PathFile: pathFile, RunnerTemp: filepath.Join(root, "temp raw=value"),
				Mise: InstallMiseInput{Version: "2026.1.2", LinuxX64SHA256: pin, LinuxARM64SHA256: pin, DestDir: filepath.Join(root, "mise bin"), BaseURL: "https://fixture.invalid/mise"}}
			bin, tool := gitCliffBin, "aqua:orhun/git-cliff@2.6.1"

			miseDest, runID := in.Mise.DestDir, "42"
			if scenario == "git-chglog defaults" {
				in.Backend, in.RunID, in.Mise.DestDir = gitChglogBin, "  ", ""
				in.BinHome, in.PathFile, in.RunnerTemp = "renderer bin", runnerPathName, "temp raw=value"
				in.Mise.BaseURL = "http://fixture.invalid/mirrors/mise%20cache/"
				bin, tool = gitChglogBin, "aqua:git-chglog/git-chglog@0.15.4"
				miseDest, runID = filepath.Join(home, ".local", "bin"), strconv.Itoa(os.Getpid())
			}

			misePath := filepath.Join(miseDest, "mise")

			scratch := filepath.Join(root, "temp raw=value")
			if scenario == "OS temp fallback" || scenario == "linked OS temp fallback" {
				in.RunnerTemp, scratch = "", filepath.Join(root, "os-temp")
				if scenario == "linked OS temp fallback" {
					alias := filepath.Join(root, "os-temp-alias")
					changelogSymlink(t, scratch, alias)

					for _, key := range []string{"TMPDIR", "TEMP", "TMP"} {
						t.Setenv(key, alias)
					}
				}
			}

			prepare := filepath.Join(scratch, "reusable-ci-prepare-mise-"+runID)
			stale := filepath.Join(prepare, "data", "stale")
			writeChangelogCanary(t, stale, "owned stale scratch\n", 0o600)
			writeChangelogCanary(t, pathFile, "prior PATH\n", 0o640)

			priorPath, err := os.Stat(pathFile)
			if err != nil {
				t.Fatal(err)
			}

			var (
				installDir        string
				fixturesUnchanged func()
			)

			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}

			executable, err = filepath.EvalSymlinks(executable)
			if err != nil {
				t.Fatal(err)
			}

			selected := filepath.Join(binHome, bin)

			var previousLink os.FileInfo

			if scenario == "existing selected link" {
				if err = os.Mkdir(binHome, 0o700); err != nil {
					t.Fatal(err)
				}

				changelogSymlink(t, executable, selected)

				previousLink, err = os.Lstat(selected)
				if err != nil {
					t.Fatal(err)
				}
			}

			if scenario == "occupied publication" {
				writeChangelogCanary(t, selected, "unrelated occupied file\n", 0o700)
			}

			var (
				out    bytes.Buffer
				events []string
			)

			arch := "x64"
			if runtime.GOARCH != "amd64" {
				arch = runtime.GOARCH
			}

			archivePath := "/v2026.1.2/mise-v2026.1.2-linux-" + arch + "-musl.tar.gz"
			requestPath, requestURI := "/mise"+archivePath, "/mise"+archivePath

			url := "https://fixture.invalid" + requestURI
			if scenario == "git-chglog defaults" {
				requestPath, requestURI = "/mirrors/mise cache"+archivePath, "/mirrors/mise%20cache"+archivePath
				url = "http://fixture.invalid" + requestURI
			}

			installOutput := "Installing mise 2026.1.2 (" + arch + ")...\n"
			adoptOutput := installOutput + "mise 2026.1.2 installed to " + misePath + "\n"

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			blockChangelogHTTP(t, func(req *http.Request) (*http.Response, error) {
				events = append(events, "HTTP "+req.Method+" "+req.URL.String())
				if req.Method != http.MethodGet || req.URL.String() != url || req.URL.Path != requestPath || req.URL.RequestURI() != requestURI || req.Context() != ctx {
					t.Errorf("unexpected request/context: %s %s path=%q uri=%q", req.Method, req.URL, req.URL.Path, req.URL.RequestURI())

					return &http.Response{StatusCode: http.StatusForbidden, Body: http.NoBody, Header: make(http.Header)}, nil
				}

				if out.String() != installOutput {
					t.Errorf("download output = %q", out.String())
				}

				assertChangelogFile(t, pathFile, "prior PATH\n", 0o640)
				assertChangelogFile(t, stale, "owned stale scratch\n", 0o600)
				assertChangelogAbsent(t, misePath)

				status, body := http.StatusOK, archive

				switch scenario {
				case "HTTP denied":
					status = http.StatusForbidden
				case "checksum mismatch":
					body = append(bytes.Clone(archive), 'x')
				case "late install obstacle":
					writeChangelogCanary(t, filepath.Join(misePath, "keep"), "late install obstacle\n", 0o600)
				}

				return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header), ContentLength: int64(len(body))}, nil
			})

			parentEnv := os.Environ()

			wantEnv := slices.Clone(parentEnv)
			for index, entry := range wantEnv {
				name, _, _ := strings.Cut(entry, "=")
				switch name {
				case runnerPathName:
					wantEnv[index] = "PATH=" + binHome + string(os.PathListSeparator) + emptyPath
				case miseCacheKey:
					wantEnv[index] = name + "=" + filepath.Join(prepare, cacheSubdir)
				case miseConfigKey:
					wantEnv[index] = name + "=" + filepath.Join(prepare, configSubdir)
				case miseDataKey:
					wantEnv[index] = name + "=" + filepath.Join(prepare, dataSubdir)
				case miseStateKey:
					wantEnv[index] = name + "=" + filepath.Join(prepare, stateSubdir)
				}
			}

			slices.Sort(wantEnv)

			installErr := fmt.Errorf("owned install refusal: %w", os.ErrPermission)
			whereErr := fmt.Errorf("owned where refusal: %w", errs.ErrUnsupported)
			runner := changelogLifecycleRunner{
				setBin: func(path string) {
					events = append(events, "SetBin "+path)
					assertChangelogFile(t, path, changelogMiseBytes, 0o755)
					assertChangelogFile(t, stale, "owned stale scratch\n", 0o600)

					if out.String() != adoptOutput {
						t.Errorf("adoption output = %q", out.String())
					}
				},
				run: func(gotCtx context.Context, env []string, args ...string) (string, error) {
					events = append(events, "Run "+strings.Join(args, " "))

					if gotCtx != ctx {
						t.Error("runner lost caller context")
					}

					actualEnv := slices.Clone(env)
					slices.Sort(actualEnv)

					if !reflect.DeepEqual(actualEnv, wantEnv) {
						t.Errorf("runner env = %q, want %q", actualEnv, wantEnv)
					}

					assertChangelogAbsent(t, stale)
					assertChangelogAbsent(t, filepath.Join(root, "version.json"))
					assertChangelogFile(t, pathFile, "prior PATH\n", 0o640)

					for _, name := range []string{cacheSubdir, configSubdir, dataSubdir, stateSubdir} {
						if info, statErr := os.Lstat(filepath.Join(prepare, name)); statErr != nil || !info.IsDir() {
							t.Errorf("prepare %s: %v", name, statErr)
						}
					}

					if scenario != "occupied publication" && scenario != "existing selected link" {
						assertChangelogAbsent(t, selected)
					}

					if reflect.DeepEqual(args, []string{"--no-config", "install", tool}) {
						if scenario == "install failure" {
							return "", installErr
						}

						dataDir := envLookup(env, miseDataKey)
						if dataDir != filepath.Join(prepare, dataSubdir) {
							t.Errorf("install data directory escaped the owned expected tree: %q", dataDir)

							return "", errs.ErrUsage
						}

						installDir = filepath.Join(dataDir, "installs", "renderer")

						candidateDir := installDir
						if bin == gitChglogBin {
							candidateDir = filepath.Join(installDir, "bin")
						}

						writeChangelogCanary(t, filepath.Join(candidateDir, "unselected"), "inert sibling, never execute\n", 0o700)

						if scenario != "missing binary" {
							changelogSymlink(t, executable, filepath.Join(candidateDir, bin))
						}

						fixturesUnchanged = changelogUnchanged(t, installDir)

						return "owned mise install output", nil
					}

					if !reflect.DeepEqual(args, []string{"--no-config", "where", tool}) {
						t.Errorf("unexpected mise argv: %q", args)

						return "", errs.ErrUsage
					}

					switch scenario {
					case "where failure":
						return "", whereErr
					case "empty where":
						return " \n", nil
					case "where outside root":
						// A real renderer outside the isolated tree: its location, not
						// the file it names, is what makes it unpublishable.
						outside := filepath.Join(root, "outside install")
						if mkdirErr := os.MkdirAll(outside, 0o700); mkdirErr != nil {
							t.Fatal(mkdirErr)
						}

						changelogSymlink(t, executable, filepath.Join(outside, bin))

						return outside + "\n", nil
					case "late PATH obstacle":
						if removeErr := os.Remove(pathFile); removeErr != nil {
							t.Fatal(removeErr)
						}

						if mkdirErr := os.Mkdir(pathFile, 0o700); mkdirErr != nil {
							t.Fatal(mkdirErr)
						}
					case "version canceled":
						cancel()
					}

					return " \n" + installDir + "\n ", nil
				},
			}
			err = InstallChangelogRenderer(ctx, runner, &out, in)
			wantEvents := []string{"HTTP GET " + url}
			wantOutput := installOutput

			var wantErr error

			switch scenario {
			case "HTTP denied":
				wantErr = errs.ErrDependencyUnavailable
			case "checksum mismatch":
				wantErr = errs.ErrValidation
			case "malformed archive":
				wantErr = errs.ErrMalformedInput
			case "late install obstacle":
				wantErr = os.ErrExist
			default:
				wantEvents = append(wantEvents, "SetBin "+misePath, "Run --no-config install "+tool)
				wantOutput = adoptOutput

				assertChangelogFile(t, misePath, changelogMiseBytes, 0o755)
				assertChangelogAbsent(t, stale)

				if scenario == "install failure" {
					wantErr = installErr

					break
				}

				wantEvents = append(wantEvents, "Run --no-config where "+tool)
				wantOutput += "owned mise install output\n"

				switch scenario {
				case "where failure":
					wantErr = whereErr
				case "empty where", "where outside root", "occupied publication", "late PATH obstacle", "version failure":
					wantErr = errs.ErrValidation
				case "missing binary":
					wantErr = errs.ErrMissingInput
				case "version canceled":
					wantErr = context.Canceled
				}
			}

			if scenario == "late install obstacle" {
				// Rename over a directory is an OS error; preserve its concrete cause.
				var pathErr *os.LinkError
				if !errors.As(err, &pathErr) {
					t.Errorf("late install error lost cause: %v", err)
				}
			} else if !errors.Is(err, wantErr) {
				t.Errorf("result = %v, want %v", err, wantErr)
			}

			published := scenario == "git-cliff success" || scenario == "git-chglog defaults" || scenario == "OS temp fallback" || scenario == "linked OS temp fallback" || scenario == "existing selected link" || scenario == "version failure" || scenario == "version canceled"

			invoked := published && scenario != "version canceled"
			if invoked { //nolint:nestif // actual child record, env and exit cause are checked together.
				body, readErr := os.ReadFile(filepath.Join(root, "version.json"))
				if readErr != nil {
					t.Fatal(readErr)
				}

				var record changelogVersionInvocation
				if decodeErr := json.Unmarshal(body, &record); decodeErr != nil {
					t.Fatal(decodeErr)
				}

				if !reflect.DeepEqual(record.Args, []string{selected, "--version"}) || record.PathBytes != "prior PATH\n"+binHome+"\n" || record.LinkTarget != executable {
					t.Errorf("version invocation = %+v", record)
				}

				slices.Sort(record.Env)
				slices.Sort(parentEnv)

				if !reflect.DeepEqual(record.Env, parentEnv) {
					t.Errorf("version environment = %q, want raw parent env %q", record.Env, parentEnv)
				}

				if info, statErr := os.Stat(filepath.Join(root, "version.json")); statErr != nil || info.Mode() != 0o600 {
					t.Errorf("private record mode: %v", statErr)
				}

				events = append(events, "version")
				wantEvents = append(wantEvents, "version")

				if scenario == "version failure" {
					var exitErr *exec.ExitError
					if !errors.As(err, &exitErr) || exitErr.ExitCode() != 17 {
						t.Errorf("version exit cause = %v", err)
					}
				} else {
					wantOutput += "owned version stdout\nowned version stderr\n"
				}
			} else {
				assertChangelogAbsent(t, filepath.Join(root, "version.json"))
			}

			assertChangelogEvents(t, events, wantEvents)

			if out.String() != wantOutput {
				t.Errorf("output = %q, want %q", out.String(), wantOutput)
			}

			switch {
			case published:
				if target, readErr := os.Readlink(selected); readErr != nil || target != executable {
					t.Errorf("selected link = %q: %v", target, readErr)
				}

				assertChangelogFile(t, pathFile, "prior PATH\n"+binHome+"\n", 0o640)

				if previousLink != nil {
					currentLink, statErr := os.Lstat(selected)
					if statErr != nil || !os.SameFile(previousLink, currentLink) || previousLink.Mode() != currentLink.Mode() {
						t.Errorf("existing selected link was replaced: %v", statErr)
					}
				}
			case scenario == "occupied publication":
				assertChangelogFile(t, selected, "unrelated occupied file\n", 0o700)
			default:
				assertChangelogAbsent(t, selected)
			}

			if !published && scenario != "late PATH obstacle" {
				assertChangelogFile(t, pathFile, "prior PATH\n", 0o640)
			}

			if scenario == "late PATH obstacle" {
				if info, statErr := os.Lstat(pathFile); statErr != nil || info.Mode() != os.ModeDir|0o700 {
					t.Errorf("late PATH obstacle changed: %v", statErr)
				}
			}

			if scenario != "late PATH obstacle" {
				current, statErr := os.Stat(pathFile)
				if statErr != nil || !os.SameFile(priorPath, current) {
					t.Errorf("PATH identity changed: %v", statErr)
				}
			}

			if len(wantEvents) == 1 {
				assertChangelogFile(t, stale, "owned stale scratch\n", 0o600)

				if scenario == "late install obstacle" {
					assertChangelogFile(t, filepath.Join(misePath, "keep"), "late install obstacle\n", 0o600)
				} else {
					assertChangelogAbsent(t, misePath)
				}
			}

			if scenario == "late install obstacle" {
				installedEntries, readErr := os.ReadDir(miseDest)
				if readErr != nil || len(installedEntries) != 1 || installedEntries[0].Name() != "mise" {
					t.Errorf("failed install left temporary files: %v: %v", installedEntries, readErr)
				}
			}

			entries, readErr := os.ReadDir(binHome)

			wantCount := 0
			if published || scenario == "occupied publication" {
				wantCount = 1
			}

			if readErr != nil || len(entries) != wantCount || (wantCount == 1 && entries[0].Name() != bin) {
				t.Errorf("bin home contents = %v: %v", entries, readErr)
			}

			if fixturesUnchanged != nil {
				fixturesUnchanged()
			}
		})
	}
}
