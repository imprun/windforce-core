type DeliverInput = {
  orderId: string;
  partnerToken: string;
};

type WindforceContext = {
  action: string;
  input: unknown;
  job: { id: string };
  logger: { info(...args: unknown[]): void };
  variables: {
    get(path: string, scope?: "workspace" | "app"): Promise<string>;
    set(path: string, value: string, options: { operationId: string }): Promise<{ revision: number }>;
  };
  resources: {
    get(path: string, scope?: "workspace" | "app"): Promise<unknown>;
    set(path: string, value: unknown, resourceType: string, options: { operationId: string }): Promise<{ revision: number }>;
  };
};

export async function main(ctx: WindforceContext) {
	if (ctx.action === "deliver") {
		const input = ctx.input as DeliverInput;
		return {
			orderId: input.orderId,
			secretResolved: !input.partnerToken.startsWith("$var:"),
			secretLength: input.partnerToken.length,
		};
	}

	if (ctx.action === "refresh") {
		const storageState = JSON.stringify({
			cookies: [{ name: "session", value: "cookie-secret-value", domain: "example.test", path: "/" }],
			origins: [{ origin: "https://example.test", localStorage: [{ name: "token", value: "local-storage-secret-value" }] }],
		});
		const variable = await ctx.variables.set("sessions/playwright", storageState, {
			operationId: `variable-${ctx.job.id}`,
		});
		const resource = await ctx.resources.set(
			"sessions/meta",
			{ label: "ready", storageState: "$var@app:sessions/playwright" },
			"browser-session@1",
			{ operationId: `resource-${ctx.job.id}` },
		);
		ctx.logger.info("refreshed session", storageState);
		return { variableRevision: variable.revision, resourceRevision: resource.revision };
	}

	if (ctx.action === "consume") {
		const storageState = await ctx.variables.get("sessions/playwright", "app");
		const resource = await ctx.resources.get("sessions/meta", "app");
		ctx.logger.info("consumed session", storageState, resource);
		return { storageState, resource };
	}

	throw new Error(`unknown action: ${ctx.action}`);
}
