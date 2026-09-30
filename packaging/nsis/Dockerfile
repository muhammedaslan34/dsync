# makensis for building the Windows installer on Linux, where NSIS usually
# isn't packaged. Used through packaging/nsis/makensis.
FROM debian:bookworm-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends nsis osslsigncode ca-certificates \
 && rm -rf /var/lib/apt/lists/*
ENTRYPOINT ["makensis"]
