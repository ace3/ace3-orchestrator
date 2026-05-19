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
    && npm cache clean --force
COPY --from=build /out/mini-paperclip /usr/local/bin/mini-paperclip
EXPOSE 8081
ENTRYPOINT ["mini-paperclip"]
