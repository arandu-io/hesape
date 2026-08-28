"""Decode the rendered symbols in this directory with a reader that shares no
code with the package, and report what came back.

This is how the golden matrices next to it were confirmed. It is not part of
the Go test run and nothing in the module depends on it.

    go test ./qr -run TestFixtures -qr.update
    python3 qr/testdata/crosscheck.py qr/testdata

It needs OpenCV, which is not a dependency of anything here:

    python3 -m pip install opencv-python

Every line has to say OK. A line that does not means the renderer produced a
symbol a reader cannot read, whatever the Go tests say.
"""

import collections
import glob
import os
import sys

import cv2


def main(directory):
    expected = {}
    for path in glob.glob(os.path.join(directory, "*.expected")):
        name = os.path.basename(path)[: -len(".expected")]
        with open(path) as handle:
            expected[name] = handle.read()
    if not expected:
        sys.exit(f"no .expected files in {directory}; run the generator first")

    classic = cv2.QRCodeDetector()
    aruco = cv2.QRCodeDetectorAruco()

    results = collections.OrderedDict()
    failures = 0
    for path in sorted(glob.glob(os.path.join(directory, "*.pgm"))):
        base = os.path.basename(path)[: -len(".pgm")]
        name, style, scale = base.split(".")
        image = cv2.imread(path, cv2.IMREAD_GRAYSCALE)
        want = expected[name]
        got = [
            classic.detectAndDecode(image)[0] == want,
            aruco.detectAndDecode(image)[0] == want,
        ]
        if not all(got):
            failures += 1
        results.setdefault(f"{name} {style}", {})[scale] = "".join(
            letter if ok else "-" for letter, ok in zip("CA", got)
        )

    for key, row in results.items():
        cells = " ".join(f"{scale}={value}" for scale, value in sorted(row.items()))
        print(f"{key:24s} {cells}")
    print(f"\n{len(results)} styles, {failures} readings failed")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1] if len(sys.argv) > 1 else "."))
