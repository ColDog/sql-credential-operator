FROM alpine:3.8

RUN apk upgrade --update --no-cache

USER nobody

ADD build/_output/bin/sql-credential-operator /usr/local/bin/sql-credential-operator
