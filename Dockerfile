FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /dummypay ./cmd/dummypay

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=build /dummypay /dummypay
EXPOSE 8080
ENTRYPOINT ["/dummypay"]