#!/usr/bin/env bash
set -euo pipefail

ROOT="${1:-/tmp/agency-s8-fixtures}"
WORKSPACE="$ROOT/workspace"

rm -rf "$WORKSPACE"
mkdir -p "$WORKSPACE/docs" "$WORKSPACE/src" "$WORKSPACE/data" "$WORKSPACE/scripts" "$WORKSPACE/tests" "$WORKSPACE/tmp"

cat > "$WORKSPACE/README.md" <<'EOF'
# Agency S8 Fixture Workspace

This repository exists only for runner fixture capture.

- `src/math.js` contains simple math helpers
- `docs/notes.md` is safe to edit
- `data/large.txt` contains deterministic numbered lines
- `scripts/fail.sh` fails on purpose
- `tests/check.sh` performs a lightweight verification
EOF

cat > "$WORKSPACE/docs/notes.md" <<'EOF'
fixture-notes
alpha
beta
EOF

cat > "$WORKSPACE/src/math.js" <<'EOF'
function add(a, b) {
  return a + b;
}

module.exports = {
  add,
};
EOF

cat > "$WORKSPACE/src/app.js" <<'EOF'
const { add } = require("./math");

function main() {
  console.log(add(2, 3));
}

if (require.main === module) {
  main();
}
EOF

cat > "$WORKSPACE/scripts/fail.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
echo "fixture failure: intentional" >&2
exit 7
EOF

cat > "$WORKSPACE/tests/check.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
grep -q 'function add' ./src/math.js
grep -q 'function subtract' ./src/math.js
grep -q 'subtract' ./README.md
echo "check ok"
EOF

chmod +x "$WORKSPACE/scripts/fail.sh" "$WORKSPACE/tests/check.sh"

python3 - <<'PY' > "$WORKSPACE/data/large.txt"
for i in range(1, 401):
    print(f"{i:03d}: fixture line {i}")
PY

(
  cd "$WORKSPACE"
  git init -q
  git config user.name "Agency Fixture"
  git config user.email "fixture@example.com"
  git add .
  git commit -q -m "fixture workspace seed"
)

echo "Created fixture workspace: $WORKSPACE"
