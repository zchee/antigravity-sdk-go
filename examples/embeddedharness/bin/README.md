# Embedded harness binary

The `localharness` file in this directory is a placeholder so the example
builds. Before running the example, replace it with a real localharness binary
for your target platform.

## Sourcing a binary

The localharness binary is shipped as part of the upstream Python package on
PyPI. To extract one without installing the package globally:

```sh
pip3 download google-antigravity --no-deps --only-binary=:all: -d /tmp/agdl
unzip -o /tmp/agdl/google_antigravity-*.whl -d /tmp/agextract
cp /tmp/agextract/google/antigravity/bin/localharness ./localharness
chmod +x ./localharness
```

PyPI ships per-platform wheels. On a different OS/arch you will get a
different binary; pin the wheel filename to the platform you want to ship.

## Per-platform layout (production)

For a real downstream that ships multiple platforms, lay the binaries out per
GOOS/GOARCH and select at runtime:

```
bin/
  linux-amd64/localharness
  linux-arm64/localharness
  darwin-amd64/localharness
  darwin-arm64/localharness
```

Then embed all of them and pick the right one in your HarnessProvider:

```go
//go:embed bin/*/localharness
var harnessFS embed.FS

func extractHarness(_ context.Context) (string, func(), error) {
    name := fmt.Sprintf("bin/%s-%s/localharness", runtime.GOOS, runtime.GOARCH)
    data, err := harnessFS.ReadFile(name)
    if err != nil {
        return "", nil, fmt.Errorf("no embedded harness for %s/%s: %w",
            runtime.GOOS, runtime.GOARCH, err)
    }
    // ... write data to a tempfile, chmod 0700, return (path, cleanup, nil)
}
```

Embedded binaries inflate your final Go binary by ~34 MB per platform. Only
embed the platforms you actually ship.
