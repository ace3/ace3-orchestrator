FROM golang:1.23-alpine AS build

WORKDIR /src
RUN apk add --no-cache git
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/mini-paperclip ./cmd/mini-paperclip

FROM alpine:3.20

RUN apk add --no-cache ca-certificates git nodejs npm postgresql-client \
    && npm install -g @anthropic-ai/claude-code@2.1.143 @openai/codex@0.130.0 \
    && npm cache clean --force \
    && addgroup -S app && adduser -S -G app -h /home/app app \
    && mkdir -p /var/lib/mini-paperclip/skills-cache /var/lib/mini-paperclip/worktrees /var/lib/mini-paperclip/backups /home/app/.config \
    && chown -R app:app /var/lib/mini-paperclip /home/app
COPY --from=build /out/mini-paperclip /usr/local/bin/mini-paperclip
USER app
ENV HOME=/home/app
EXPOSE 8081
ENTRYPOINT ["mini-paperclip"]
