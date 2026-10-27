# Статический бинарник без CGO в distroless: ни shell, ни пакетов, процесс не от root.
FROM golang:1.26-alpine@sha256:9ce231b28998b32e116927c61aa5c406183061d4f3f2d61b292c88e7f6bdf7ca AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/subproxy . && mkdir /out/certs

FROM gcr.io/distroless/static-debian13:nonroot@sha256:516757106de35205efb70b91ba03f6f1298424a0678c603255cf2852a7000087
COPY --from=build /out/subproxy /subproxy
# Каталог сертификатов принадлежит пользователю контейнера: новый именованный том наследует владельца.
COPY --from=build --chown=nonroot:nonroot /out/certs /data/certs
COPY routing.json /srv/routing/routing.json
EXPOSE 8080 8443
ENTRYPOINT ["/subproxy"]
