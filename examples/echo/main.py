def main(ctx):
    return {
        "ok": True,
        "app": ctx.app,
        "action": ctx.action,
        "input": ctx.input,
    }
