#!/usr/bin/env bash
# Render the Homebrew Formula after release archives and checksums have been created.

set -euo pipefail

version="${1:-}"
checksums="${2:-}"
output="${3:-}"
if [[ ! "${version}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || [[ ! -f "${checksums}" ]] || [[ -z "${output}" ]]; then
  echo "Usage: scripts/render-homebrew-formula.sh <version> <checksums.txt> <output.rb>" >&2
  exit 2
fi

checksum_for() {
  local file="$1"
  awk -v file="${file}" '$2 == file || $2 == "*" file { print $1; exit }' "${checksums}"
}

darwin_arm64="$(checksum_for "starcat_${version}_darwin_arm64.tar.gz")"
darwin_amd64="$(checksum_for "starcat_${version}_darwin_amd64.tar.gz")"
linux_arm64="$(checksum_for "starcat_${version}_linux_arm64.tar.gz")"
linux_amd64="$(checksum_for "starcat_${version}_linux_amd64.tar.gz")"
for checksum in "${darwin_arm64}" "${darwin_amd64}" "${linux_arm64}" "${linux_amd64}"; do
  if [[ ! "${checksum}" =~ ^[0-9a-f]{64}$ ]]; then
    echo "Homebrew Formula generation failed: missing release checksum" >&2
    exit 1
  fi
done

# Homebrew infers the stable version from URLs; duplicating it fails strict audit.
mkdir -p "$(dirname "${output}")"
{
  echo 'class Starcat < Formula'
  echo '  desc "Cross-platform CLI and MCP bridge for Starcat"'
  echo '  homepage "https://github.com/starcat-app/starcat-cli"'
  echo '  license "MIT"'
  echo
  echo '  on_macos do'
  echo '    if Hardware::CPU.arm?'
  echo "      url \"https://github.com/starcat-app/starcat-cli/releases/download/${version}/starcat_${version}_darwin_arm64.tar.gz\""
  echo "      sha256 \"${darwin_arm64}\""
  echo '    else'
  echo "      url \"https://github.com/starcat-app/starcat-cli/releases/download/${version}/starcat_${version}_darwin_amd64.tar.gz\""
  echo "      sha256 \"${darwin_amd64}\""
  echo '    end'
  echo '  end'
  echo
  echo '  on_linux do'
  echo '    if Hardware::CPU.arm?'
  echo "      url \"https://github.com/starcat-app/starcat-cli/releases/download/${version}/starcat_${version}_linux_arm64.tar.gz\""
  echo "      sha256 \"${linux_arm64}\""
  echo '    else'
  echo "      url \"https://github.com/starcat-app/starcat-cli/releases/download/${version}/starcat_${version}_linux_amd64.tar.gz\""
  echo "      sha256 \"${linux_amd64}\""
  echo '    end'
  echo '  end'
  echo
  echo '  def install'
  echo '    bin.install "starcat"'
  echo '  end'
  echo
  echo '  test do'
  echo '    assert_match "v#{version}", shell_output("#{bin}/starcat version")'
  echo '  end'
  echo 'end'
} > "${output}"
