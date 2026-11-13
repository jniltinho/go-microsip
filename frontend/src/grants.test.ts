import { expect, test } from "vite-plus/test";
import { createMock } from "./mock";

// The mock's Agent access behaves as the window's: a grant is refused for what the owner would
// refuse, listed until revoked, and every step is in the audit log.
test("the mock grants, lists, revokes and records", async () => {
  // The mock reads its scene and its skin from the page it runs in.
  Object.assign(globalThis, {
    location: { search: "" },
    localStorage: { getItem: () => null, setItem: () => undefined },
  });
  const backend = createMock();
  const before = (await backend.grants()).length;
  const spec = {
    scopes: ["control:dial"],
    destinations: ["1002"],
    incoming: [],
    minutes: 10,
    quota: 1,
  };
  const path = await backend.createGrant(spec, false);
  expect(path).toMatch(/\/grants\/grant-.*\.grant$/);
  const boot = await backend.createGrant(
    { ...spec, scopes: ["control:dnd"], destinations: [] },
    true,
  );
  expect(boot).toMatch(/\/bootstrap\/grant-.*\.json$/);
  const listed = await backend.grants();
  expect(listed.length).toBe(before + 2);
  expect(listed[0]).toMatchObject({ bootstrap: true, used: 0 });
  expect(JSON.stringify(listed)).not.toContain("capability");

  await expect(backend.createGrant({ ...spec, scopes: [] }, false)).rejects.toMatch(/permission/);
  await expect(backend.createGrant({ ...spec, scopes: ["control:shell"] }, false)).rejects.toMatch(
    /permission/,
  );
  await expect(backend.createGrant({ ...spec, minutes: 61 }, false)).rejects.toMatch(/minutes/);
  await expect(backend.createGrant({ ...spec, quota: 101 }, false)).rejects.toMatch(/call limit/);
  await expect(backend.createGrant({ ...spec, destinations: ["10 02"] }, false)).rejects.toMatch(
    /number/,
  );
  expect((await backend.grants()).length).toBe(before + 2);

  await backend.revokeGrant(listed[0].id);
  expect((await backend.grants()).map((g) => g.id)).not.toContain(listed[0].id);
  const log = await backend.auditLog();
  expect(log.slice(0, 3).map((entry) => entry.decision)).toEqual([
    "revoked",
    "bootstrap issued",
    "issued",
  ]);
  await backend.clearAudit();
  expect(await backend.auditLog()).toEqual([]);
});
