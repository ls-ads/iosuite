const ffi = require('ffi-napi');
const path = require('path');

const libPath = path.resolve(__dirname, '../../bin/libiocore' + (process.platform === 'win32' ? '.dll' : '.so'));

const lib = ffi.Library(libPath, {
    'ProcessImage': ['string', ['string', 'string']],
    'FreeString': ['void', ['pointer']]
});

module.exports = {
    processImage: function (inputPath, outputPath) {
        const err = lib.ProcessImage(inputPath, outputPath);
        if (err) {
            // In a real implementation with FreeString, we'd need to handle the pointer 
            // differently if we want to call FreeString from JS.
            // ffi-napi 'string' return type might automatically copy and we lose the pointer.
            // For this skeleton, we assume the error message is small or handled.
            throw new Error(`Go error: ${err}`);
        }
        return true;
    }
};
