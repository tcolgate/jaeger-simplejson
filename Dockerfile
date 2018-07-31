FROM golang:1.11beta2

COPY  . /src
WORKDIR /src
RUN go build -o /bin/jsjaeger .

ENTRYPOINT ["/bin/jsjaeger"]
