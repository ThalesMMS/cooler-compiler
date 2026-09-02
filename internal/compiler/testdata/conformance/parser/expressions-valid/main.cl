class Main inherits IO {
  value : Int <- 0;

  main() : Object {
    {
      value <- 1 + 2 * 3;
      out_int(value);
      self@IO.out_string("/");
      if not false then out_string("if") else out_string("bad") fi;
      while false loop value <- value + 1 pool;
      let item : Object <- new Main, negative : Int <- ~1 in
        case item of
          main : Main => { out_string("/case"); main; };
          other : Object => other;
        esac;
    }
  };
};
