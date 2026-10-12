// SPDX-License-Identifier: MIT

export async function load(url, context, nextLoad) {
  if (url.endsWith(".json") && !context.importAttributes?.type) {
    return nextLoad(url, { ...context, importAttributes: { ...context.importAttributes, type: "json" } });
  }
  return nextLoad(url, context);
}
