import { execFileSync } from "node:child_process";

function git(args) {
  return execFileSync("git", args, {
    encoding: "utf8",
    stdio: ["ignore", "pipe", "ignore"],
  }).trim();
}

try {
  const commit = git(["rev-parse", "--short=12", "HEAD"]);
  process.stdout.write(commit);
} catch {
  process.stdout.write("unknown");
}
