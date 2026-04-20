FROM gcr.io/distroless/static-debian12:nonroot
ARG TARGETOS
ARG TARGETARCH
COPY ${TARGETOS}/${TARGETARCH}/confluence-mcp /confluence-mcp
ENTRYPOINT ["/confluence-mcp"]
