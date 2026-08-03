type DeliverInput = {
  orderId: string;
  partnerToken: string;
};

type WindforceContext = {
  action: string;
  input: unknown;
};

export async function main(ctx: WindforceContext) {
  if (ctx.action !== "deliver") {
    throw new Error(`unknown action: ${ctx.action}`);
  }

  const input = ctx.input as DeliverInput;
  return {
    orderId: input.orderId,
    secretResolved: !input.partnerToken.startsWith("$var:"),
    secretLength: input.partnerToken.length,
  };
}
