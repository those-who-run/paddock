#!/bin/sh
# Render the cluster manifests under deploy/ and check them (paddock#42): `make
# deploy-render`, which `make test` and so CI's `test` job run. For each kustomization
# below it runs `kustomize build --enable-helm`, validates the output with kubeconform
# in strict mode against the Kubernetes version below, and runs the checks of
# scripts/deploy_check.py on it.
#
# kustomize, helm and kubeconform are fetched at the versions pinned here, checked
# against the sha256 written here, and kept in .cache/tools/ (git ignores it), so a
# laptop and CI render with the same tools whatever else is installed. Renovate does
# not bump them: change a version and its four checksums together, from the
# release's own checksums file.
#
# Needs curl, tar, the network (the chart, and kubeconform's schemas) and python3.
# Linux and macOS, amd64 and arm64.

set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

# The kustomizations rendered, from the root of paddock.
KUSTOMIZATIONS="deploy/nats/chart"

# The Kubernetes version the manifests are validated against.
KUBERNETES_VERSION=1.33.0

KUSTOMIZE_VERSION=5.8.3
HELM_VERSION=3.22.0
KUBECONFORM_VERSION=0.8.0

case "$(uname -s)" in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) echo "deploy-render: no pinned tools for $(uname -s)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) echo "deploy-render: no pinned tools for $(uname -m)" >&2; exit 1 ;;
esac
platform=$os-$arch

# The sha256 of each release archive, from its release's checksums file.
sha256_of() {
	case "$1 $platform" in
	"kustomize linux-amd64") echo cb9e31198d3f63b44848bd0afc4c8efc56e6cfef9c93a4a39a211940e6decd61 ;;
	"kustomize linux-arm64") echo 9867b76482cfc6d25b546abc1d2b5a32bd137505aba640649b4b378f64f6d68f ;;
	"kustomize darwin-amd64") echo d08fb85846c3d977f0e2a66e7ff45f1b5264c294685e5e726e1a8bdfe0da8dee ;;
	"kustomize darwin-arm64") echo 4cbb08e209d8fff47cd60bfb6407daaf5ee699f9b9295849a1065024fac2d1ef ;;
	"helm linux-amd64") echo 1e4ab49e429626cf6c6958d914248b78c9730803c2751b87627e171dc800e7bb ;;
	"helm linux-arm64") echo f14e804dfee240f55525b667488fe9adca349e63e00c9af634c0beb1421ac310 ;;
	"helm darwin-amd64") echo bd1d09f316558dda23698527859600936fed1371021ce0f2272d66b2fdcfa69c ;;
	"helm darwin-arm64") echo 4c9982a6cdeb458b60258df66b55398ca5b19293f6877faffe2909ad6f23dfe0 ;;
	"kubeconform linux-amd64") echo 9bc2bffbf71f261128533edaf912153948b7ff238f9a531ae6d34466ec287883 ;;
	"kubeconform linux-arm64") echo 1f53fc8e81258197a35e8603054162a5af1de8c5af13746c71ab680d9534ed87 ;;
	"kubeconform darwin-amd64") echo 71dbc87ac9f24099a62b93570e65aa06312ba6ac8aea63b7f86e9d999edf5a92 ;;
	"kubeconform darwin-arm64") echo f84f4dfbebf4a6b0b230385fa065a39ea35e02608c2b50d025dcf64775a69d67 ;;
	esac
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	else
		shasum -a 256 "$1" | cut -d' ' -f1
	fi
}

# tool NAME VERSION URL PATH-IN-ARCHIVE: the binary's path, fetched and checked the
# first time.
tool() {
	dir=.cache/tools/$1-$2-$platform
	if [ ! -x "$dir/$1" ]; then
		mkdir -p "$dir"
		archive=$dir/archive.tar.gz
		curl -fsSL -o "$archive" "$3"
		want=$(sha256_of "$1")
		have=$(sha256 "$archive")
		if [ "$have" != "$want" ]; then
			echo "deploy-render: $3 has sha256 $have, not the pinned $want" >&2
			rm -rf "$dir"
			exit 1
		fi
		tar -xzf "$archive" -C "$dir" "$4"
		[ "$4" = "$1" ] || mv "$dir/$4" "$dir/$1"
		rm -f "$archive"
	fi
	echo "$root/$dir/$1"
}

kustomize=$(tool kustomize "$KUSTOMIZE_VERSION" \
	"https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2Fv$KUSTOMIZE_VERSION/kustomize_v${KUSTOMIZE_VERSION}_${os}_$arch.tar.gz" \
	kustomize)
helm=$(tool helm "$HELM_VERSION" \
	"https://get.helm.sh/helm-v$HELM_VERSION-$platform.tar.gz" \
	"$platform/helm")
kubeconform=$(tool kubeconform "$KUBECONFORM_VERSION" \
	"https://github.com/yannh/kubeconform/releases/download/v$KUBECONFORM_VERSION/kubeconform-$platform.tar.gz" \
	kubeconform)

out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT INT TERM

for k in $KUSTOMIZATIONS; do
	rendered=$out/$(printf '%s' "$k" | tr / _).yaml
	echo "deploy-render: $k"
	"$kustomize" build --enable-helm --helm-command "$helm" "$k" >"$rendered"
	"$kubeconform" -strict -summary -kubernetes-version "$KUBERNETES_VERSION" "$rendered"
	python3 scripts/deploy_check.py "$k" "$rendered"
done
