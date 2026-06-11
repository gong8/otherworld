import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { World } from "./World";
import "./world.css";

export const metadata: Metadata = {
  title: "the otherworld",
  description: "talk to your agent; watch it deal with the world.",
};

export default async function Page({
  searchParams,
}: {
  searchParams: Promise<{ [key: string]: string | string[] | undefined }>;
}) {
  const params = await searchParams;
  const raw = params.scope ?? "household";
  if (raw !== "household" && raw !== "street") notFound();
  const scope = `scope:${raw}`;

  return <World key={scope} scope={scope} />;
}
