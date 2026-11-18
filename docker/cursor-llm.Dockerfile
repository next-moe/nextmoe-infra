# OpenAI-compatible /v1/chat/completions over the Cursor SDK's local runtime
# (tools disabled). Dokploy runs it as the cursor-llm service, the ai gateway's
# "cursor" provider; callers send the Cursor API key as the bearer token.
FROM node:24-trixie-slim

WORKDIR /app
COPY docker/cursor-llm/package.json docker/cursor-llm/package-lock.json ./
RUN npm ci --omit=dev --ignore-scripts && npm cache clean --force
COPY docker/cursor-llm/server.mjs ./

ENV HOME=/home/node \
    LLM_WORKSPACE=/home/node/ws \
    NODE_OPTIONS=--disable-warning=ExperimentalWarning

USER node
EXPOSE 8790
CMD ["node", "server.mjs"]
