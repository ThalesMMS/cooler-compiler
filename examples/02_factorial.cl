class Main inherits IO {
  factorial(n : Int) : Int {
    if n <= 1 then
      1
    else
      n * factorial(n - 1)
    fi
  };

  main() : Object {
    {
      out_string("5! = ");
      out_int(factorial(5));
      out_string("\n");
    }
  };
};
