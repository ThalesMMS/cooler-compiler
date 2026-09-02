# Fuzz regressions

This directory stores minimized source or structured mutation inputs that exposed a crash, mismatch,
invalid span, nondeterminism, or another compiler robustness defect. Every seed
must be listed in `manifest.json` with its bug class, originating issue, and
expected invariant. Go's generated fuzz cache remains outside the repository;
only reviewed, minimized reproductions belong here.
