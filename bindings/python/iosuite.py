import ctypes
import os
import sys

class IOSuite:
    def __init__(self, lib_path: str = None):
        if lib_path is None:
            # Default to local bin directory
            ext = ".so" if sys.platform != "win32" else ".dll"
            lib_path = os.path.abspath(os.path.join(os.path.dirname(__file__), "../../../bin/libiocore" + ext))
        
        if not os.path.exists(lib_path):
            raise FileNotFoundError(f"Shared library not found at {lib_path}")

        self.lib = ctypes.CDLL(lib_path)
        
        # Setup ProcessImage
        self.lib.ProcessImage.argtypes = [ctypes.c_char_p, ctypes.c_char_p]
        self.lib.ProcessImage.restype = ctypes.c_char_p
        
        # Setup FreeString
        self.lib.FreeString.argtypes = [ctypes.c_char_p]
        self.lib.FreeString.restype = None

    def process_image(self, input_path: str, output_path: str):
        err_ptr = self.lib.ProcessImage(input_path.encode('utf-8'), output_path.encode('utf-8'))
        if err_ptr:
            err_msg = err_ptr.decode('utf-8')
            self.lib.FreeString(err_ptr)
            raise Exception(f"Go error: {err_msg}")
        return True
