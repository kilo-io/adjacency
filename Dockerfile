FROM golang:1.18-alpine as build
RUN apk --no-cach add gcc libc-dev
WORKDIR /adjacency
COPY . /adjacency
RUN GOOS=linux go build --mod=vendor -o ./adjacency

FROM alpine
WORKDIR /
COPY --from=build /adjacency/adjacency .
ENTRYPOINT ["/adjacency"]
