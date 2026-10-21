// Records a page through the Chrome DevTools screencast and encodes the frames
// to H.264 with ffmpeg. Playwright's own recordVideo writes low-bitrate VP8 at
// CSS-pixel size, which is too soft for product videos. The screencast hands
// back each frame at device-pixel size with the time it was painted.
//
// Chrome only sends a frame when the page repaints, so frames arrive at
// irregular intervals. Each frame is held on screen until the next one was
// painted, through the ffmpeg concat demuxer's per-file durations, and the
// result is resampled to a constant frame rate.
import fs from "node:fs";
import path from "node:path";
import { spawn } from "node:child_process";
import ffmpegPath from "ffmpeg-static";

const FPS = 60;

export async function startRecording(page, { width, height, scale, workDir }) {
  fs.rmSync(workDir, { recursive: true, force: true });
  fs.mkdirSync(workDir, { recursive: true });

  const cdp = await page.context().newCDPSession(page);
  const frames = [];
  const writes = [];

  cdp.on("Page.screencastFrame", ({ data, metadata, sessionId }) => {
    const file = path.join(workDir, `frame-${String(frames.length).padStart(6, "0")}.jpg`);
    frames.push({ file, at: metadata.timestamp });
    writes.push(fs.promises.writeFile(file, Buffer.from(data, "base64")));
    cdp.send("Page.screencastFrameAck", { sessionId }).catch(() => {});
  });

  await cdp.send("Page.startScreencast", {
    format: "jpeg",
    quality: 95,
    maxWidth: width * scale,
    maxHeight: height * scale,
    everyNthFrame: 1,
  });
  const startedAt = Date.now();

  return {
    /** Milliseconds since recording began, for the timeline. */
    elapsed: () => Date.now() - startedAt,

    async stop(outFile) {
      await cdp.send("Page.stopScreencast");
      const stoppedAt = Date.now() / 1000;
      await Promise.all(writes);
      await cdp.detach();
      if (frames.length === 0) throw new Error("the screencast produced no frames");

      const lines = ["ffconcat version 1.0"];
      frames.forEach((frame, i) => {
        const next = frames[i + 1]?.at ?? stoppedAt;
        lines.push(`file '${frame.file}'`, `duration ${Math.max(next - frame.at, 1 / FPS)}`);
      });
      // The concat demuxer ignores the last entry's duration unless the file
      // is listed once more.
      lines.push(`file '${frames.at(-1).file}'`);
      const list = path.join(workDir, "frames.txt");
      fs.writeFileSync(list, lines.join("\n"));

      await ffmpeg([
        "-y",
        "-f", "concat",
        "-safe", "0",
        "-i", list,
        "-vf", `fps=${FPS},scale=trunc(iw/2)*2:trunc(ih/2)*2,format=yuv420p`,
        "-c:v", "libx264",
        "-preset", "slow",
        "-crf", "16",
        "-movflags", "+faststart",
        outFile,
      ]);
      fs.rmSync(workDir, { recursive: true, force: true });
    },
  };
}

function ffmpeg(args) {
  return new Promise((resolve, reject) => {
    const proc = spawn(ffmpegPath, ["-hide_banner", "-loglevel", "error", ...args], {
      stdio: ["ignore", "inherit", "inherit"],
    });
    proc.on("error", reject);
    proc.on("exit", (code) =>
      code === 0 ? resolve() : reject(new Error(`ffmpeg exited with code ${code}`)),
    );
  });
}
