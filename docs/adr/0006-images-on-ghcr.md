# 0006: v2 images on GitHub's container registry

**Status:** Accepted, 2026-10-22. Asked for by #16 (R8). Supersedes
[ADR 0001](0001-v2-architecture.md), point 4, on images only.

## Context

[ADR 0001](0001-v2-architecture.md), point 4, records "Images are
`muldoon/twr-<repo>`", on Docker Hub, and its table names each component's
image that way. That followed v1, whose images are on Docker Hub.

#16 adds the organisation's reusable image workflow, which builds, smoke-tests
and pushes every v2 image with an SBOM and provenance, from each repository's
`release` environment. Its open question was where to push: Docker Hub, which
needs a registry secret in every publishing repository, or GitHub's container
registry, which does not. The maintainer chose GitHub's container registry
when he triaged #16 on 2026-10-22. An accepted record is not edited, so this
record carries that choice.

## Decision

### 1. v2 images are on `ghcr.io/those-who-run`

- **Name:** `ghcr.io/those-who-run/twr-<repo>:X.Y.Z`, for example
  `ghcr.io/those-who-run/twr-ecu:0.1.0`. The `twr-` prefix and the version
  tag stay as ADR 0001 had them; only the registry and the namespace change.
- **Pushed with the publishing job's own `GITHUB_TOKEN`,** with
  `packages: write` on that job only. There is no registry secret to create,
  store or rotate, so the `release` environment holds no registry
  credentials.
- **Each image links to its repository** through the
  `org.opencontainers.image.source` label, so its package page points at the
  code it was built from.
- **Public.** The maintainer makes each new package public once, after its
  first push, so clusters and laptops pull without a login or a pull secret.
- **amd64 only** (`linux/amd64`) until a need for another platform shows.
  The workflow's `platforms` input stays for when it does.
- **v1 stays on Docker Hub** as it is. This record is about v2 only.

The workflow is `image.yml` in
[`those-who-run/.github`](https://github.com/those-who-run/.github/blob/main/.github/workflows/image.yml)
(#16), and its README there says how to verify an image's attestations.

Settles: #16 (R3, R8 and the registry and platform questions). The image
names in #21, #29, #39, #44, #55, #131 and any other issue that still says
`muldoon/twr-*` follow this record when they are worked.

## What #16 does because of this

#16's workflow pushes to the name in point 1. This record and its row in
[`README.md`](README.md) are #16's R8; ADR 0001's status line and its index
row say "Superseded in part by 0006 (point 4)", as the
[process](README.md#how) describes. The rest of ADR 0001's point 4 (one
repository per component, the table's other columns, issues in `paddock`,
experiments, deployment) still holds.

## Consequences

- **No registry secret anywhere,** so there is nothing for an agent to
  touch, leak or forget to rotate, and one fewer maintainer step per
  repository.
- **One organisation holds the code, the build, the provenance and the
  image.** The attestations are GitHub's, and they can sit beside the image
  in the same registry.
- **A new package starts private.** Until the maintainer makes it public,
  a pull without credentials fails. This is a step per component, once.
- **v1 and v2 publish to different registries** while they run side by side.
  The names do not overlap, so neither can overwrite the other.

## Alternatives considered

- **Docker Hub, `muldoon/twr-*`, as ADR 0001 had it.** Familiar from v1, but
  every publishing repository needs a registry secret in its `release`
  environment, and the namespace is a personal account rather than the
  organisation.
- **Docker Hub under an organisation account.** Needs the account and still
  needs a secret in each repository.

## References

- #16, and the maintainer's triage comment on it.
- GitHub's container registry: https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry
- Publishing packages with `GITHUB_TOKEN`: https://docs.github.com/en/packages/managing-github-packages-using-github-actions-workflows/publishing-and-installing-a-package-with-github-actions
