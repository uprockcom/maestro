# Copyright 2025 Christopher O'Connell
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Multi-stage build for the Signal relay server
FROM golang:1.25 AS builder

WORKDIR /build
COPY cmd/signal-relay/ .
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /signal-relay .

FROM alpine:latest

RUN apk --no-cache add ca-certificates
COPY --from=builder /signal-relay /usr/local/bin/signal-relay

EXPOSE 80 443 8080

ENTRYPOINT ["/usr/local/bin/signal-relay"]
