module.exports = {
  metadata: {
    name: "strategy-name",
    displayName: "Strategy Display Name",
    description: "Describe the strategy's behaviour and requirements.",
    config: [
      {
        name: "threshold",
        type: "number",
        description: "Example configuration field.",
        default: 0.5,
        required: false
      }
    ],
    events: ["Trade"]
  },
  create: function (env) {
    return {
      onTrade: function (ctx, event) {
        env.helpers.log("Received trade", event.payload);
      }
    };
  }
};
