# moviestreamer

Streams videos from a Mac to devices on the LAN. Two parts:

- **CLI server** (`main.go`): serves a video file, or every video and subtitle in a folder, over HTTP with Range support for seeking.
- **TV app** (`tv/`): a Tizen web app for Samsung TVs that lists the server's videos and plays them with Samsung's AVPlay player.

## CLI server

Requires Go.

```sh
make build      # builds ./moviestreamer
make install    # builds and installs to ~/.local/bin/moviestreamer
```

Run it:

```sh
moviestreamer                  # serves ~/Downloads
moviestreamer ~/Movies         # serves a folder
moviestreamer movie.mkv        # serves one file and its subtitles
moviestreamer -port 9000 -no-qr
```

It prints the LAN address (for example `http://10.0.0.92:8080/`) and a QR code.

### Image subtitles

TVs cannot show PGS (Blu-ray image) subtitles. The first time an `.mkv` without an `.srt` or `.vtt` is played, the server runs `pgsrip rip` on it in the background. pgsrip reads the subtitles with OCR (about 10 to 20 seconds per episode) and writes `<video name>.<lang>.srt` next to the video. This needs:

```sh
brew install tesseract mkvtoolnix
uv tool install pgsrip
```

Without `pgsrip` on the PATH, the server skips this step.

Endpoints:

| Path | Purpose |
| --- | --- |
| `/` | HTML list with mpv-android play links (folder mode only) |
| `/api/list` | JSON list of videos grouped by folder, each with its `.srt`/`.vtt` subtitles. Used by the TV app. |
| `/<path>` | The file itself, with Range support |

## TV app

### One-time setup

1. **Install the Tizen Studio web CLI** to `~/tizen-studio` (installer from https://download.tizen.org/sdk/Installer/, for example `web-cli_Tizen_Studio_6.1_macos-64.bin --accept-license ~/tizen-studio`). On Apple Silicon it needs Rosetta.
2. **Enable developer mode on the TV**: open Apps, type `12345` on the remote, turn Developer mode on and set Host PC IP to the Mac's LAN IP, then restart the TV.
3. **Create a signing profile** named `moviestreamer`:

   ```sh
   T=~/tizen-studio/tools/ide/bin/tizen
   $T certificate -a moviestreamer -p moviestreamer -f moviestreamer
   $T security-profiles add -n moviestreamer -a ~/tizen-studio-data/keystore/author/moviestreamer.p12 -p moviestreamer
   ```

   A plain Tizen certificate is enough for the current TV (UN58TU7000, Tizen 6.0). Other models may need a Samsung certificate from the Tizen Studio Certificate Manager.

### Build and install

```sh
make tv               # TV at 10.0.0.170
make tv TV=10.0.0.42  # another TV
```

This signs `tv/` into `build/MovieStreamer.wgt`, connects with `sdb` and installs it. To launch it remotely:

```sh
~/tizen-studio/tools/ide/bin/tizen run -p MvStrmTV01.MovieStreamer -s 10.0.0.170:26101
```

To find a TV's IP, any Samsung TV answers `http://<ip>:8001/api/v2/` with its model, `developerMode` and `developerIP`.

### Using it

On first launch, type the server address (for example `10.0.0.92:8080`) and press Done. Red changes it later.

| Key | List | Player |
| --- | --- | --- |
| ▲ ▼ | Move | Cycle subtitles / audio track |
| OK | Play | Pause / resume |
| ◀ ▶ | | Pick a position on the seek bar (10 s steps, 30 s and 60 s when held). OK jumps there, Back cancels. |
| ⏪ ⏩ | | Same as ◀ ▶ in 60 s steps |
| Back | Exit | Back to list |

Playback resumes where it stopped. Subtitles come from `.srt`/`.vtt` files next to the video (the server pairs them by file name) and from text tracks inside the video. On a first play, image subtitles are still being converted: press ▲ after a few seconds and the app picks up the new `.srt`.
