# How To: Install azldev from COPR

`azldev` is packaged as an RPM and published through [Fedora COPR](https://copr.fedorainfracloud.org/).
Two repositories are provided:

| Repository | Contents | Rebuilt on |
|------------|----------|-----------|
| `azldev-stable` | Tagged, released versions only | A new `azldev-X.Y.Z-R` git tag is pushed |
| `azldev-dev` | Latest development snapshot | Every commit pushed to the `main` branch |

Use `azldev-stable` unless you specifically want to test unreleased changes.

## Install

Replace `@OWNER` with the COPR owner (a user such as `jdoe` or a group such as
`@azurelinux`).

Stable:

```bash
sudo dnf copr enable @OWNER/azldev-stable
sudo dnf install azldev
```

Development snapshots:

```bash
sudo dnf copr enable @OWNER/azldev-dev
sudo dnf install azldev
```

Verify the installation:

```bash
azldev version
```

Because development snapshot versions sort *below* the next real release, a
machine tracking `azldev-dev` upgrades cleanly to `azldev-stable` once the
corresponding release is published.

> COPR currently targets **Fedora 43** (Azure Linux 4 buildroots are not yet
> available in COPR). Fedora 44 and Azure Linux 3.0 may be added as additional
> build targets.

## For maintainers: how the COPR repos are wired to git

The packaging lives in this repository:

- `azldev.spec` — the RPM spec, at the repository root (required so tito
  archives the whole Go source tree).
- `.tito/` — [tito](https://github.com/rpm-software-management/tito)
  configuration used to turn a git commit into an SRPM.

Both COPR projects build from this repository using tito; they differ only in
*which commit* they build and *what triggers* them.

### One-time COPR setup

Create the two projects (Fedora 43, x86_64 + aarch64):

```bash
copr-cli create azldev-stable \
  --chroot fedora-43-x86_64 --chroot fedora-43-aarch64 \
  --description "Stable azldev builds from tagged releases"

copr-cli create azldev-dev \
  --chroot fedora-43-x86_64 --chroot fedora-43-aarch64 \
  --description "Development azldev builds from every commit on main"
```

Add an SCM package to each, pointing at this repository. The stable project
builds from the latest tito **tag**; the dev project builds the **HEAD** of
`main` (tito's `--test` mode):

```bash
# Stable: SRPM build method "tito" -> builds the latest azldev-X.Y.Z-R tag.
copr-cli add-package-scm azldev-stable \
  --name azldev \
  --clone-url https://github.com/microsoft/azure-linux-dev-tools \
  --spec azldev.spec \
  --type git \
  --method tito

# Dev: SRPM build method "tito_test" -> builds the given committish (main HEAD).
copr-cli add-package-scm azldev-dev \
  --name azldev \
  --clone-url https://github.com/microsoft/azure-linux-dev-tools \
  --commit main \
  --spec azldev.spec \
  --type git \
  --method tito_test
```

### Auto-build webhooks

Each COPR project exposes a webhook URL under **Settings → Integrations**. Add
them as GitHub webhooks (repository **Settings → Webhooks**, content type
`application/json`):

- **`azldev-dev`** — subscribe to **push** events. COPR only rebuilds when the
  pushed branch matches the package committish (`main`), so every merge to
  `main` produces a fresh dev RPM.
- **`azldev-stable`** — subscribe to **Branch or tag creation** events. Pushing
  an `azldev-X.Y.Z-R` tag triggers a stable rebuild at that tag.

### Cutting a stable release

The `vX.Y.Z` tags used for `go install` are separate from the `azldev-X.Y.Z-R`
tags that drive packaging. To publish a stable RPM:

```bash
# Bump Version in azldev.spec, regenerate vendored deps, and tag.
go mod vendor && git add vendor
tito tag                 # bumps X.Y.Z, updates %changelog, creates the tag
git push --follow-tags   # pushing the azldev-* tag triggers the stable build
```

Use `tito tag --keep-version` to package the version already in the spec
without bumping it.

### Offline builds and vendored dependencies

COPR's mock buildroots have no network access during the RPM build, so the
source tarball must be self-contained. The spec builds with `-mod=vendor`, which
requires a committed `vendor/` directory. Regenerate it with `go mod vendor`
(and commit the result) whenever `go.mod`/`go.sum` change; `tito tag` is a good
time to do this. Building the `azldev` binary does **not** require running
`go generate` — the `//go:generate` directives only produce test mocks.

You can reproduce an SRPM locally to validate the setup:

```bash
tito build --srpm          # from the latest tag
tito build --srpm --test   # from your latest commit
```
