FROM golang:1.22.0

WORKDIR /app

COPY go.sum go.mod ./

RUN go mod download -x

COPY . .

RUN GCO_ENABLED=0 go build -ldflags "-s -w" -o scheduler main.go

FROM alpine:3.17

COPY --from=0 /app/scheduler /scheduler

ENTRYPOINT ["/scheduler"]