// Custom AI tool: receives the tool input as JSON on stdin, prints the result.
let input = "";
process.stdin.on("data", (d) => (input += d));
process.stdin.on("end", () => {
  const { image } = JSON.parse(input || "{}");
  const tag = (image || "").split(":")[1] || "latest";
  console.log(tag === "latest" ? `${image} uses the mutable :latest tag; pin a version or digest.` : `${image} is pinned to tag ${tag}.`);
});
