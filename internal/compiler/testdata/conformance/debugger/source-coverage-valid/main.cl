class Main inherits IO {
  descend(n : Int) : Int { if n = 0 then 0 else descend(n - 1) fi };
  main() : Object {
    let x : Int <- case descend(2) of value : Int => value; esac in {
      while x < 2 loop x <- x + 1 pool;
      out_int(x);
    }
  };
};
