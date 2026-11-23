import { describe, expect, test } from "vitest";

import type { PricingModel } from "../../types";
import { buildSupportedParameters } from "../mock-stats";

function names(model: Partial<PricingModel>) {
  return buildSupportedParameters(model as PricingModel).map((p) => p.name);
}

describe("buildSupportedParameters", () => {
  test("a managed model lists exactly the parameters the gateway measured", () => {
    expect(
      names({
        model_name: "anthropic.claude-opus-5-5",
        model_type: "text",
        supported_parameters: ["max_tokens", "stop", "stream"],
      }),
    ).toEqual(["max_tokens", "stop", "stream"]);
  });

  test("an embedding model lists its own parameters", () => {
    expect(
      names({
        model_name: "cohere.embed-english-v3",
        model_type: "embedding",
        supported_parameters: ["input", "encoding_format"],
      }),
    ).toEqual(["input", "encoding_format"]);
  });

  test("tool_choice lists only the values the model accepts", () => {
    const parameters = buildSupportedParameters({
      model_name: "anthropic.claude-opus-5-5",
      model_type: "text",
      supported_parameters: ["max_tokens", "tools", "tool_choice"],
      tool_choices: ["auto", "none"],
    } as PricingModel);
    expect(parameters.map((p) => p.name)).toEqual([
      "max_tokens",
      "tools",
      "tool_choice",
    ]);
    const toolChoice = parameters.find((p) => p.name === "tool_choice");
    expect(toolChoice?.enumValues).toEqual(["auto", "none"]);
    expect(toolChoice?.descriptionKey).toBe(
      "Tool-choice policy; this model cannot be made to call a tool by name",
    );
  });

  test("a model that takes every tool_choice keeps the full description", () => {
    const toolChoice = buildSupportedParameters({
      model_name: "amazon.nova-pro-v1:0",
      model_type: "text",
      supported_parameters: ["tool_choice"],
      tool_choices: ["auto", "none", "required", "function"],
    } as PricingModel)[0];
    expect(toolChoice.enumValues).toEqual(["auto", "none", "required"]);
    expect(toolChoice.descriptionKey).toBe(
      "Tool-choice policy or specific tool name",
    );
  });
});
