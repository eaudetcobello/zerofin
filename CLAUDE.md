# moviestreamer

See README.md for building, running and deploying. Summary: `make install` for the CLI, `make tv` to sign and install the TV app.

## TV app constraints

- The TV (Samsung UN58TU7000 at 10.0.0.170, Tizen 6.0) runs Chromium 76. Write ES5 JavaScript and avoid newer CSS such as `inset`, `gap` on flex, or `aspect-ratio`.
- Video plays through `webapis.avplay`, not a `<video>` element, so HTML5 player libraries do not apply. The video plane sits behind the page; the player screen must keep a transparent background.
- The Samsung on-screen keyboard sends key code 65376 for Done, not 13.
- Media and color keys must be registered with `tizen.tvinputdevice.registerKey` before they reach `keydown`.
- `tizen package` writes signature files into `tv/`; `make tv` removes them. Never commit them.
- After changing `tv/`, deploy with `make tv` and relaunch with `~/tizen-studio/tools/ide/bin/tizen run -p MvStrmTV01.MovieStreamer -s 10.0.0.170:26101`.
