import hashlib
import os
import sys
import urllib.request

repo, revision, dest = sys.argv[1:4]
files = {
    "model.onnx": "35f23693620b668f4d53fd3c62bf65e40af739bc52c7eb0fbc49258b58d065b6",
    "selected_tags.csv": "298633d94d0031d2081c0893f29c82eab7f0df00b08483ba8f29d1e979441217",
}
os.makedirs(dest, exist_ok=True)
for name, want in files.items():
    path = os.path.join(dest, name)
    urllib.request.urlretrieve(f"https://huggingface.co/{repo}/resolve/{revision}/{name}", path)
    got = hashlib.sha256(open(path, "rb").read()).hexdigest()
    if got != want:
        sys.exit(f"{name}: sha256 {got}, want {want}")
    os.chmod(path, 0o444)
    print(name, got)
