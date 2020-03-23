# FROM golang:onbuild
FROM ubuntu:16.04
ARG DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
    && apt-get upgrade -y \
    && apt-get install -y

RUN apt-get install --assume-yes apt-utils
RUN apt-get install -y ca-certificates

ADD ./prebid-cache /app/prebid-cache
ADD ./config.yaml /app/

WORKDIR /app
CMD ["./prebid-cache"]
