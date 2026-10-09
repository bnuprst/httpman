require('./modules.js');
require('./runtime.js');
// Postman's sandbox has always shipped Sugar.js 1.4, which extends native
// prototypes (e.g. responseBody.has("x"), [1, 2].none(3)). Old collections
// rely on it.
require('sugar');
require('./pm.js');
