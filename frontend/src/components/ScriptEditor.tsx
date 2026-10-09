import Code from './Code';

const PRE_SNIPPETS: [string, string][] = [
  ['Get an environment variable', 'pm.environment.get("variable_key");'],
  ['Get a global variable', 'pm.globals.get("variable_key");'],
  ['Get a variable', 'pm.variables.get("variable_key");'],
  ['Get a collection variable', 'pm.collectionVariables.get("variable_key");'],
  ['Set an environment variable', 'pm.environment.set("variable_key", "variable_value");'],
  ['Set a global variable', 'pm.globals.set("variable_key", "variable_value");'],
  ['Set a collection variable', 'pm.collectionVariables.set("variable_key", "variable_value");'],
  ['Clear an environment variable', 'pm.environment.unset("variable_key");'],
  ['Clear a global variable', 'pm.globals.unset("variable_key");'],
  ['Add a header', 'pm.request.headers.add({ key: "X-Header", value: "value" });'],
  ['Send a request', 'pm.sendRequest("https://postman-echo.com/get", function (err, response) {\n    console.log(response.json());\n});'],
  ['Timestamp variable', 'pm.environment.set("timestamp", Date.now());'],
  ['HMAC signature (CryptoJS)', 'const signature = CryptoJS.HmacSHA256(pm.request.body.toString(), pm.environment.get("secret")).toString();\npm.request.headers.upsert({ key: "X-Signature", value: signature });'],
];

const TEST_SNIPPETS: [string, string][] = [
  ['Status code: Code is 200', 'pm.test("Status code is 200", function () {\n    pm.response.to.have.status(200);\n});'],
  ['Response body: Contains string', 'pm.test("Body matches string", function () {\n    pm.expect(pm.response.text()).to.include("string_you_want_to_search");\n});'],
  ['Response body: JSON value check', 'pm.test("Your test name", function () {\n    var jsonData = pm.response.json();\n    pm.expect(jsonData.value).to.eql(100);\n});'],
  ['Response body: Is equal to a string', 'pm.test("Body is correct", function () {\n    pm.response.to.have.body("response_body_string");\n});'],
  ['Response headers: Content-Type header check', 'pm.test("Content-Type is present", function () {\n    pm.response.to.have.header("Content-Type");\n});'],
  ['Response time is less than 200ms', 'pm.test("Response time is less than 200ms", function () {\n    pm.expect(pm.response.responseTime).to.be.below(200);\n});'],
  ['Status code: Successful POST request', 'pm.test("Successful POST request", function () {\n    pm.expect(pm.response.code).to.be.oneOf([201, 202]);\n});'],
  ['Status code: Code name has string', 'pm.test("Status code name has string", function () {\n    pm.response.to.have.status("Created");\n});'],
  ['Response body: Convert XML body to a JSON Object', 'var jsonObject = xml2Json(responseBody);'],
  ['Use Tiny Validator for JSON data', 'var schema = {\n    "items": {\n        "type": "boolean"\n    }\n};\nvar data1 = [true, false];\n\npm.test("Schema is valid", function () {\n    pm.expect(tv4.validate(data1, schema)).to.be.true;\n});'],
  ['Validate JSON schema', 'const schema = {\n    type: "object",\n    required: ["id"],\n    properties: { id: { type: "integer" } }\n};\n\npm.test("Schema is valid", function () {\n    pm.response.to.have.jsonSchema(schema);\n});'],
  ['Set an environment variable from response', 'pm.environment.set("token", pm.response.json().token);'],
  ['Set next request', 'pm.execution.setNextRequest("request_name");'],
];

interface Props {
  value: string;
  onChange: (v: string) => void;
  kind: 'prerequest' | 'test';
  onSave?: () => void;
  onSend?: () => void;
}

export default function ScriptEditor({ value, onChange, kind, onSave, onSend }: Props) {
  const snippets = kind === 'test' ? TEST_SNIPPETS : PRE_SNIPPETS;
  return (
    <div className="script-editor">
      <div className="script-code">
        <Code value={value} onChange={onChange} lang="javascript" scripts onSave={onSave} onSend={onSend} />
      </div>
      <div className="snippets">
        <p className="muted small">
          {kind === 'test'
            ? 'Test scripts are written in JavaScript and run after the response is received.'
            : 'Pre-request scripts are written in JavaScript and run before the request is sent.'}
        </p>
        <div className="snippets-title">SNIPPETS</div>
        {snippets.map(([label, code]) => (
          <button key={label} className="snippet" onClick={() => onChange((value && !value.endsWith('\n') ? value + '\n' : value) + code + '\n')}>
            {label}
          </button>
        ))}
      </div>
    </div>
  );
}
