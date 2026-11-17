FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY *.go index.html ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /zerofin .

# pgsrip converts PGS (image) subtitles to .srt with tesseract and mkvextract.
# opencv, which pgsrip uses, needs libgl1 and libglib2.0-0.
FROM python:3.13-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      tesseract-ocr tesseract-ocr-eng tesseract-ocr-fra mkvtoolnix libgl1 libglib2.0-0 \
 && rm -rf /var/lib/apt/lists/* \
 && pip install --no-cache-dir pgsrip
COPY --from=build /zerofin /usr/local/bin/zerofin
EXPOSE 8080
ENTRYPOINT ["zerofin"]
CMD ["/media"]
