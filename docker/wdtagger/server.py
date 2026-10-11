import csv
import io
import json
import os
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import numpy as np
import onnxruntime as ort
from PIL import Image

MODEL_DIR = os.environ.get("WD_MODEL_DIR", "/model")
MODEL_ID = os.environ.get("WD_MODEL_ID", "")
THREADS = int(os.environ.get("WD_THREADS", "4"))
PORT = int(os.environ.get("WD_PORT", "8080"))
MAX_BYTES = 24 << 20

RATINGS = ("general", "sensitive", "questionable", "explicit")
TAGS = (
    "nipples", "areola_slip", "nude", "completely_nude", "topless", "bottomless",
    "sex", "penis", "pussy", "fellatio", "vaginal", "anal", "cum", "paizuri",
    "mosaic_censoring", "bar_censor", "censored",
    "underwear", "panties", "bra", "lingerie", "swimsuit", "bikini",
)

names = [row["name"] for row in csv.DictReader(open(os.path.join(MODEL_DIR, "selected_tags.csv")))]
index = {name: i for i, name in enumerate(names)}
missing = [n for n in RATINGS + TAGS if n not in index]
if missing:
    raise SystemExit(f"selected_tags.csv lacks {missing}")

options = ort.SessionOptions()
options.intra_op_num_threads = THREADS
options.inter_op_num_threads = 1
session = ort.InferenceSession(os.path.join(MODEL_DIR, "model.onnx"), options, providers=["CPUExecutionProvider"])
input_name = session.get_inputs()[0].name
size = session.get_inputs()[0].shape[1]
run_lock = threading.Lock()


def prepare(raw):
    image = Image.open(io.BytesIO(raw))
    image.seek(0)
    image = image.convert("RGBA")
    canvas = Image.new("RGBA", image.size, (255, 255, 255))
    canvas.alpha_composite(image)
    image = canvas.convert("RGB")
    side = max(image.size)
    square = Image.new("RGB", (side, side), (255, 255, 255))
    square.paste(image, ((side - image.size[0]) // 2, (side - image.size[1]) // 2))
    if side != size:
        square = square.resize((size, size), Image.BICUBIC)
    return np.asarray(square, dtype=np.float32)[:, :, ::-1][None, ...]


def tag(raw):
    tensor = prepare(raw)
    with run_lock:
        probs = session.run(None, {input_name: tensor})[0][0]
    return {
        "model": MODEL_ID,
        "rating": {n: round(float(probs[index[n]]), 4) for n in RATINGS},
        "tags": {n: round(float(probs[index[n]]), 4) for n in TAGS},
    }


class Handler(BaseHTTPRequestHandler):
    def _send(self, status, body):
        data = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        if self.path == "/healthz":
            self._send(200, {"model": MODEL_ID})
        else:
            self._send(404, {"error": "not found"})

    def do_POST(self):
        if self.path != "/v1/tag":
            self._send(404, {"error": "not found"})
            return
        length = int(self.headers.get("Content-Length") or 0)
        if length <= 0 or length > MAX_BYTES:
            self._send(413 if length > MAX_BYTES else 400, {"error": f"body of {length} bytes"})
            return
        raw = self.rfile.read(length)
        try:
            result = tag(raw)
        except (OSError, ValueError) as err:
            self._send(422, {"error": f"undecodable image: {err}"})
            return
        self._send(200, result)

    def log_message(self, fmt, *args):
        pass


if __name__ == "__main__":
    print(f"wdtagger {MODEL_ID} threads={THREADS} input={size} listening :{PORT}", flush=True)
    ThreadingHTTPServer(("0.0.0.0", PORT), Handler).serve_forever()
