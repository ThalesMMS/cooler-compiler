class Main inherits IO {
  divide(value : Int) : Int { 100 / value };

  main() : Object {
    {
      out_string("about to divide\n");
      out_int(divide(0));
    }
  };
};
