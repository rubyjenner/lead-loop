FROM golang:1.22 AS build
WORKDIR /src
COPY . .
RUN go mod tidy && CGO_ENABLED=0 go build -o /api .

FROM gcr.io/distroless/static
COPY --from=build /api /api
EXPOSE 8080
CMD ["/api"]
