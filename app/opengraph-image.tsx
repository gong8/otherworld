import { ImageResponse } from "next/og";
import { readFile } from "node:fs/promises";
import path from "node:path";

export const alt = "the otherworld — the world beside the world. it is already speaking.";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";

export default async function OpengraphImage() {
  const fontsDir = path.join(process.cwd(), "assets", "fonts");
  const [italic, roman] = await Promise.all([
    readFile(path.join(fontsDir, "eb-garamond-500-italic.ttf")),
    readFile(path.join(fontsDir, "eb-garamond-400-normal.ttf")),
  ]);

  // equal fixed-width side slots keep the centered label on the 600px axis
  const sideSlot = 150;

  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          flexDirection: "column",
          backgroundColor: "#ECE9E1",
          color: "#1D1B17",
          fontFamily: "EB Garamond",
        }}
      >
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
            padding: "52px 72px 0",
            fontSize: 19,
            letterSpacing: 6,
            color: "#6B665C",
          }}
        >
          <div style={{ display: "flex", width: sideSlot }}>◇</div>
          <div style={{ display: "flex" }}>[ A NOTICE TO RESIDENTS ]</div>
          <div style={{ display: "flex", width: sideSlot, justifyContent: "flex-end" }}>
            № 0001
          </div>
        </div>
        <div
          style={{
            flex: 1,
            display: "flex",
            flexDirection: "column",
            alignItems: "center",
            justifyContent: "center",
            gap: 40,
          }}
        >
          <div style={{ fontSize: 104, fontStyle: "italic", fontWeight: 500 }}>
            the otherworld
          </div>
          <div
            style={{
              display: "flex",
              flexDirection: "column",
              alignItems: "center",
              gap: 12,
              fontSize: 21,
              letterSpacing: 10,
              color: "#4A463F",
            }}
          >
            <div style={{ display: "flex", paddingLeft: 10 }}>THE WORLD BESIDE THE WORLD.</div>
            <div style={{ display: "flex", paddingLeft: 10 }}>IT IS ALREADY SPEAKING.</div>
          </div>
        </div>
        <div
          style={{
            display: "flex",
            justifyContent: "center",
            paddingBottom: 48,
            fontSize: 16,
            letterSpacing: 6,
            color: "#6B665C",
          }}
        >
          NO ACTION IS REQUIRED
        </div>
      </div>
    ),
    {
      ...size,
      fonts: [
        { name: "EB Garamond", data: italic, weight: 500, style: "italic" },
        { name: "EB Garamond", data: roman, weight: 400, style: "normal" },
      ],
    },
  );
}
