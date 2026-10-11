# Image-safety tagger for cmd/classify-image-safety -mode grade: SmilingWolf's
# WD ViT tagger v3 (Apache-2.0) on CPU behind a one-route HTTP server. The model
# is pinned by revision and checksum (fetch_model.py) because the grade
# thresholds in cmd/classify-image-safety/wd.go were calibrated against exactly
# these weights.
FROM python:3.12-slim-bookworm

ARG WD_REPO=SmilingWolf/wd-vit-tagger-v3
ARG WD_REVISION=7f6b584d0bd3f55c4531f14ba3d4761b2bccdc0f

RUN pip install --no-cache-dir onnxruntime==1.31.0 numpy==2.5.4 pillow==12.3.0

COPY docker/wdtagger/fetch_model.py /tmp/fetch_model.py
RUN python /tmp/fetch_model.py "$WD_REPO" "$WD_REVISION" /model && rm /tmp/fetch_model.py
COPY docker/wdtagger/server.py /app/server.py

ENV WD_MODEL_DIR=/model \
    WD_MODEL_ID=${WD_REPO}@${WD_REVISION} \
    WD_THREADS=4 \
    PYTHONUNBUFFERED=1

RUN python -c "import sys; sys.argv=['x']; exec(open('/app/server.py').read().split('class Handler')[0]); import io; from PIL import Image; b=io.BytesIO(); Image.new('RGB',(64,48),(200,100,50)).save(b,'WEBP'); r=tag(b.getvalue()); assert set(r['rating'])=={'general','sensitive','questionable','explicit'}, r; print('selftest', r['rating'])"

USER 10001
EXPOSE 8080
CMD ["python", "/app/server.py"]
