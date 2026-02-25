FROM alpine:3.21
RUN apk add --no-cache curl jq bash
COPY entrypoint.sh /entrypoint.sh
COPY wait-for-opensearch.sh /wait-for-opensearch.sh
RUN chmod +x /entrypoint.sh /wait-for-opensearch.sh
ENTRYPOINT ["/entrypoint.sh"]
